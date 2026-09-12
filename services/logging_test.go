package services

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreLogOutput 在用例结束后恢复全局 log 输出
func restoreLogOutput(t *testing.T) {
	t.Helper()

	flags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
}

// 日志必须写进用户配置目录，而不是工作目录 ——
// 双击启动 .app 时工作目录是 /，写相对路径会直接失败
func TestSetupLoggingWritesToConfigDir(t *testing.T) {
	dir := withTempConfigDir(t)
	restoreLogOutput(t)

	path, err := SetupLogging()
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	want := filepath.Join(dir, settingsDirName, logFileName)
	if path != want {
		t.Errorf("日志路径不对\n期望: %s\n实际: %s", want, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("日志文件未创建: %v", err)
	}
	if _, err := os.Stat(logFileName); err == nil {
		t.Error("日志被写进了工作目录")
	}
}

// 写进去的内容要真的落盘 —— 否则用户拿到的是个空文件
func TestLogOutputReachesFile(t *testing.T) {
	withTempConfigDir(t)
	restoreLogOutput(t)

	path, err := SetupLogging()
	if err != nil {
		t.Fatal(err)
	}

	log.Println("查询门店 R683 失败: 接口返回 HTTP 541")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "HTTP 541") {
		t.Errorf("日志内容未落盘，实际内容:\n%s", data)
	}
}

// 工作目录不可写时也必须能写日志（等同访达启动 .app 的场景）
func TestSetupLoggingWorksWhenCwdUnwritable(t *testing.T) {
	dir := t.TempDir()
	restoreLogOutput(t)

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

	if _, err := SetupLogging(); err != nil {
		t.Fatalf("工作目录不可写时初始化失败: %v", err)
	}
}

// 追加而非覆盖：上一次运行的记录不应被新一次启动抹掉
func TestSetupLoggingAppends(t *testing.T) {
	withTempConfigDir(t)
	restoreLogOutput(t)

	path, err := SetupLogging()
	if err != nil {
		t.Fatal(err)
	}
	log.Println("第一次运行")

	if _, err := SetupLogging(); err != nil {
		t.Fatal(err)
	}
	log.Println("第二次运行")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "第一次运行") || !strings.Contains(content, "第二次运行") {
		t.Errorf("日志应追加而非覆盖，实际内容:\n%s", content)
	}
}

// 超过上限时轮转，避免无限增长占用用户磁盘
func TestRotateWhenTooLarge(t *testing.T) {
	dir := withTempConfigDir(t)
	restoreLogOutput(t)

	path := filepath.Join(dir, settingsDirName, logFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, logRotateSize+1), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SetupLogging(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("超限的日志应被轮转为 .1: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= logRotateSize {
		t.Errorf("轮转后应从空文件重新开始，实际 %d 字节", info.Size())
	}
}

// 未超限时不应轮转，否则每次启动都会丢掉上次的记录
func TestNoRotateWhenSmall(t *testing.T) {
	dir := withTempConfigDir(t)
	restoreLogOutput(t)

	path := filepath.Join(dir, settingsDirName, logFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("上次运行的记录\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SetupLogging(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("未超限不应轮转")
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "上次运行的记录") {
		t.Error("未超限时旧内容应保留")
	}
}

func TestLogDirIsSettingsDir(t *testing.T) {
	dir := withTempConfigDir(t)

	got, err := LogDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, settingsDirName); got != want {
		t.Errorf("日志目录应与配置同级\n期望: %s\n实际: %s", want, got)
	}
}
