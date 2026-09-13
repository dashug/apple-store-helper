package services

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/golang-module/carbon"
)

const (
	historyFileName = "history.jsonl"

	// maxHistoryEntries 是保留的记录条数上限。
	// 只保留最近的部分：再早的记录对「该盯哪家店」这个判断已经没有参考价值，
	// 却会让文件无限增长。
	maxHistoryEntries = 500
)

// historyMu 保证并发写入不会交错成半行
var historyMu sync.Mutex

// HistoryEntry 是一次「看到有货」的记录。
//
// 只记录命中，不记录每一轮的无货结果 —— 后者每 5 秒产生一批，
// 淹没掉真正有价值的信息，而用户关心的是「哪家店什么时候出过货」。
type HistoryEntry struct {
	Time    carbon.DateTime `json:"time"`
	Area    string          `json:"area"`
	Store   string          `json:"store"`
	Product string          `json:"product"`
}

// HistoryPath 返回记录文件路径，与配置、日志同目录
func HistoryPath() (string, error) {
	dir, err := configDirFn()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, settingsDirName, historyFileName), nil
}

// RecordInStock 追加一条有货记录。
// 失败只记日志：记录丢一条，远不如打断监听严重。
func RecordInStock(area string, item ListenItem) {
	entry := HistoryEntry{
		Time:    carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)},
		Area:    area,
		Store:   item.Store.CityStoreName,
		Product: item.Product.Title,
	}

	if err := appendHistory(entry); err != nil {
		log.Println("写入有货记录失败:", err)
	}
}

func appendHistory(entry HistoryEntry) error {
	path, err := HistoryPath()
	if err != nil {
		return err
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return trimHistory(path)
}

// trimHistory 超出上限时裁掉最早的记录
func trimHistory(path string) error {
	entries, err := readHistory(path)
	if err != nil || len(entries) <= maxHistoryEntries {
		return err
	}

	entries = entries[len(entries)-maxHistoryEntries:]

	var buf strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// readHistory 按写入顺序读出记录，跳过损坏的行
func readHistory(path string) ([]HistoryEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var entries []HistoryEntry

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// 单行损坏不应让整份记录不可读
			continue
		}
		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}

// LoadHistory 返回全部记录，最近的排在最前
func LoadHistory() ([]HistoryEntry, error) {
	path, err := HistoryPath()
	if err != nil {
		return nil, err
	}

	historyMu.Lock()
	entries, err := readHistory(path)
	historyMu.Unlock()

	if err != nil {
		return nil, err
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Time.Timestamp() > entries[j].Time.Timestamp()
	})

	return entries, nil
}

// StoreHitCount 统计各门店出现有货的次数，用于判断该重点盯哪几家
func StoreHitCount(entries []HistoryEntry) map[string]int {
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Store]++
	}

	return counts
}
