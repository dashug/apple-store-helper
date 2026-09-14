package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"apple-store-helper/model"
)

func sampleItem(store, product string) ListenItem {
	return ListenItem{
		Store:   model.Store{StoreNumber: "R683", CityStoreName: store},
		Product: model.Product{Code: "MJYH4CH/A", Title: product},
	}
}

// 记录必须写进用户配置目录。访达启动 .app 时工作目录是 /，写相对路径会失败。
func TestRecordWritesToConfigDir(t *testing.T) {
	dir := withTempConfigDir(t)

	RecordInStock("中国大陆", sampleItem("上海-环球港", "iphone18promax"))

	want := filepath.Join(dir, settingsDirName, historyFileName)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("记录未写入预期位置 %s: %v", want, err)
	}
	if _, err := os.Stat(historyFileName); err == nil {
		t.Error("记录被写进了工作目录")
	}
}

func TestRecordAndLoadRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	RecordInStock("中国大陆", sampleItem("上海-环球港", "iphone18promax - 勃艮第酒红色 - 1tb"))

	entries, err := LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("应有 1 条记录，实际 %d", len(entries))
	}

	got := entries[0]
	if got.Area != "中国大陆" || got.Store != "上海-环球港" {
		t.Errorf("字段不一致: %+v", got)
	}
	if !strings.Contains(got.Product, "勃艮第酒红色") {
		t.Errorf("型号丢失: %q", got.Product)
	}
	if got.Time.IsZero() {
		t.Error("时间为空")
	}
}

// 最近的排最前，否则用户要翻到最后才能看到刚发生的命中
func TestLoadHistoryNewestFirst(t *testing.T) {
	withTempConfigDir(t)

	for _, s := range []string{"第一家", "第二家", "第三家"} {
		RecordInStock("中国大陆", sampleItem(s, "iphone18pro"))
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("应有 3 条，实际 %d", len(entries))
	}

	for i := 1; i < len(entries); i++ {
		if entries[i-1].Time.Timestamp() < entries[i].Time.Timestamp() {
			t.Errorf("顺序不对：第 %d 条早于第 %d 条", i-1, i)
		}
	}
}

// 追加而非覆盖：重启后上次的记录不应消失
func TestRecordAppends(t *testing.T) {
	withTempConfigDir(t)

	RecordInStock("中国大陆", sampleItem("上海-环球港", "A"))
	RecordInStock("中国大陆", sampleItem("北京-三里屯", "B"))

	entries, _ := LoadHistory()
	if len(entries) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(entries))
	}
}

// 超出上限时裁掉最早的，避免文件无限增长
func TestHistoryIsTrimmed(t *testing.T) {
	withTempConfigDir(t)

	for i := 0; i < maxHistoryEntries+10; i++ {
		RecordInStock("中国大陆", sampleItem("上海-环球港", "iphone18pro"))
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxHistoryEntries {
		t.Errorf("应裁剪到 %d 条，实际 %d", maxHistoryEntries, len(entries))
	}
}

// 单行损坏不应让整份记录不可读
func TestCorruptLineIsSkipped(t *testing.T) {
	dir := withTempConfigDir(t)

	RecordInStock("中国大陆", sampleItem("上海-环球港", "A"))

	path := filepath.Join(dir, settingsDirName, historyFileName)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, []byte("{这不是合法 JSON\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	RecordInStock("中国大陆", sampleItem("北京-三里屯", "B"))

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("损坏行不应导致读取失败: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("应跳过损坏行后得到 2 条，实际 %d", len(entries))
	}
}

// 命中记录写在监听 goroutine 中，必须能并发安全地追加
func TestConcurrentRecording(t *testing.T) {
	withTempConfigDir(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RecordInStock("中国大陆", sampleItem("上海-环球港", "iphone18pro"))
		}()
	}
	wg.Wait()

	entries, err := LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Errorf("并发写入应得到 20 条完整记录，实际 %d", len(entries))
	}
}

func TestStoreHitCount(t *testing.T) {
	entries := []HistoryEntry{
		{Store: "上海-环球港"}, {Store: "上海-环球港"}, {Store: "北京-三里屯"},
	}

	counts := StoreHitCount(entries)
	if counts["上海-环球港"] != 2 || counts["北京-三里屯"] != 1 {
		t.Errorf("统计不对: %+v", counts)
	}
}

// 没有记录文件时返回空而不是报错，界面据此提示「还没有记录」
func TestLoadHistoryWithoutFile(t *testing.T) {
	withTempConfigDir(t)

	entries, err := LoadHistory()
	if err != nil {
		t.Errorf("无文件时不应报错: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("无文件时应返回空，实际 %d 条", len(entries))
	}
}

// 工作目录不可写时也必须能记录（等同访达启动 .app 的场景）
func TestRecordWorksWhenCwdUnwritable(t *testing.T) {
	dir := t.TempDir()

	orig := configDirFn
	configDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDirFn = orig })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	RecordInStock("中国大陆", sampleItem("上海-环球港", "iphone18pro"))

	entries, err := LoadHistory()
	if err != nil || len(entries) != 1 {
		t.Errorf("工作目录不可写时记录失败: err=%v 条数=%d", err, len(entries))
	}
}
