package services

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName = "app.log"

	// logRotateSize 是单个日志文件的大小上限。
	// 监听失败时每轮都会记录，挂一整天足以积累可观的体量。
	logRotateSize = 2 << 20 // 2 MiB
)

// LogPath 返回日志文件路径，与配置文件放在同一目录
func LogPath() (string, error) {
	dir, err := configDirFn()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, settingsDirName, logFileName), nil
}

// LogDir 返回日志所在目录，供界面上的「打开日志」使用
func LogDir() (string, error) {
	path, err := LogPath()
	if err != nil {
		return "", err
	}

	return filepath.Dir(path), nil
}

// logFile 是当前打开的日志文件。
//
// 进程退出时由操作系统回收，本不必留着引用；但 Windows 上打开着的文件
// 既删不掉也轮转不了 —— 重复调用 SetupLogging 会一路泄漏句柄，
// 而这正是三平台测试矩阵在 Windows 上抓到的失败。
var (
	logMu   sync.Mutex
	logFile *os.File
)

// SetupLogging 让日志同时写入文件。
//
// 从访达双击启动 .app 时，stdout 不指向任何用户能看到的地方，程序里
// 所有诊断信息（查询失败及其原因、推送失败、保存配置失败）都被直接
// 丢弃，用户遇到问题时一行也捞不到。
//
// 返回日志文件路径。终端运行时仍然保留 stderr 输出。
func SetupLogging() (string, error) {
	path, err := LogPath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	// 先关掉上一次打开的文件：Windows 上不关就轮转不了，
	// 重复调用也会一路泄漏句柄
	CloseLogging()

	rotateIfLarge(path)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}

	logMu.Lock()
	logFile = file
	logMu.Unlock()

	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.SetFlags(log.LstdFlags)

	return path, nil
}

// CloseLogging 关闭日志文件，并把日志输出退回 stderr。
//
// 正常运行时不需要调用 —— 进程退出即回收。它是给测试用的：
// Windows 上删不掉被打开的文件，临时目录清理会直接失败。
func CloseLogging() {
	logMu.Lock()
	file := logFile
	logFile = nil
	logMu.Unlock()

	if file == nil {
		return
	}

	log.SetOutput(os.Stderr)
	_ = file.Close()
}

// rotateIfLarge 超过上限时轮转一次，保留一份历史。
// 只保留一份是有意的：再多对排查没有帮助，却会一直占用用户磁盘。
func rotateIfLarge(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < logRotateSize {
		return
	}

	if err := os.Rename(path, path+".1"); err != nil {
		log.Println("日志轮转失败:", err)
	}
}
