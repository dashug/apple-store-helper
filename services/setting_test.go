package services

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempConfigDir 把配置目录与工作目录都指向临时目录，
// 避免测试写到真实的用户配置里
func withTempConfigDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	orig := configDirFn
	configDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDirFn = orig })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	return dir
}

func sampleSettings() UserSettings {
	return UserSettings{
		SelectedArea:    "中国大陆",
		SelectedStore:   "上海-环球港",
		SelectedProduct: "iphone18pro - 黑色 - 256gb",
		BarkNotifyUrl:   "https://api.day.app/key",
		ListenItems: map[string]ListenItem{
			"R683.MJYH4CH/A": testItem(),
		},
	}
}

func TestSaveAndLoadSettings(t *testing.T) {
	withTempConfigDir(t)

	want := sampleSettings()
	if err := SaveSettings(want); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got, err := LoadSettings()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}

	if got.SelectedArea != want.SelectedArea || got.BarkNotifyUrl != want.BarkNotifyUrl {
		t.Errorf("字段不一致: %+v", got)
	}
	if len(got.ListenItems) != 1 {
		t.Errorf("监听项丢失，实际 %d 项", len(got.ListenItems))
	}
}

// 核心回归：配置必须写进用户配置目录，而不是工作目录。
// macOS 从访达启动 .app 时工作目录是 /，写相对路径必定失败。
func TestSaveSettingsWritesToConfigDirNotCwd(t *testing.T) {
	dir := withTempConfigDir(t)

	if err := SaveSettings(sampleSettings()); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	want := filepath.Join(dir, settingsDirName, settingsFileName)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("配置未写入预期位置 %s: %v", want, err)
	}

	if _, err := os.Stat(settingsFileName); err == nil {
		t.Error("配置被写进了工作目录，路径修复未生效")
	}
}

// 工作目录不可写时也必须能保存（模拟访达启动的场景）
func TestSaveSettingsWorksWhenCwdUnwritable(t *testing.T) {
	dir := t.TempDir()

	orig := configDirFn
	configDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDirFn = orig })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// / 对普通用户不可写，等同于访达启动 .app 时的工作目录
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := SaveSettings(sampleSettings()); err != nil {
		t.Fatalf("工作目录不可写时保存失败: %v", err)
	}
	if _, err := LoadSettings(); err != nil {
		t.Fatalf("工作目录不可写时读取失败: %v", err)
	}
}

// 旧版本把配置写在工作目录，升级后应能继续读到
func TestLoadSettingsFallsBackToLegacyFile(t *testing.T) {
	withTempConfigDir(t)

	legacy := `{"selected_area":"日本","bark_notify_url":"https://api.day.app/legacy"}`
	if err := os.WriteFile(settingsFileName, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSettings()
	if err != nil {
		t.Fatalf("读取旧配置失败: %v", err)
	}
	if got.SelectedArea != "日本" || got.BarkNotifyUrl != "https://api.day.app/legacy" {
		t.Errorf("旧配置未被读取: %+v", got)
	}
}

// 新位置存在时优先于旧位置
func TestNewLocationTakesPrecedenceOverLegacy(t *testing.T) {
	withTempConfigDir(t)

	if err := os.WriteFile(settingsFileName, []byte(`{"selected_area":"旧"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(UserSettings{SelectedArea: "新"}); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectedArea != "新" {
		t.Errorf("应优先读取新位置，实际 %q", got.SelectedArea)
	}
}

// 「清空」必须同时清掉新旧两处，否则重启后旧配置又被读出来
func TestClearSettingsRemovesBothLocations(t *testing.T) {
	dir := withTempConfigDir(t)

	if err := os.WriteFile(settingsFileName, []byte(`{"selected_area":"旧"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(sampleSettings()); err != nil {
		t.Fatal(err)
	}

	if err := ClearSettings(); err != nil {
		t.Fatalf("清空失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, settingsDirName, settingsFileName)); err == nil {
		t.Error("新位置的配置未被删除")
	}
	if _, err := os.Stat(settingsFileName); err == nil {
		t.Error("旧位置的配置未被删除，重启后会被重新读出")
	}
	if _, err := LoadSettings(); err == nil {
		t.Error("清空后仍能读到配置")
	}
}

// 没有任何配置文件时应返回错误，main.go 据此走默认分支
func TestClearSettingsIsIdempotent(t *testing.T) {
	withTempConfigDir(t)

	if err := ClearSettings(); err != nil {
		t.Errorf("无配置文件时清空不应报错: %v", err)
	}
	if _, err := LoadSettings(); err == nil {
		t.Error("无配置文件时应返回错误")
	}
}
