package main

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
)

// 构造 widget 需要文本测量能力，必须先有一个 fyne app
func TestMain(m *testing.M) {
	test.NewApp()
	os.Exit(m.Run())
}

// isolateSettings 把配置目录与工作目录都指向临时目录。
// 配置现在存放在用户配置目录下，不隔离的话测试会读写使用者真实的配置。
func isolateSettings(t *testing.T) {
	t.Helper()

	// os.UserConfigDir 在各平台读取的环境变量
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func newTestWidgets() (*widget.Select, *multiSelect, *multiSelect, *widget.Entry) {
	area := services.Listen.GetArea().Title

	storeSelect := newMultiSelect("搜索门店", 150)
	storeSelect.SetOptions(services.Store.ByAreaTitleForOptions(area))

	productSelect := newMultiSelect("搜索型号", 150)
	productSelect.SetOptions(services.Product.ByAreaTitleForOptions(area))

	return widget.NewSelect(services.Area.ForOptions(), nil), storeSelect, productSelect, newBarkWidget()
}

// 重启后从缓存恢复的 Bark 地址必须同步到监听服务，
// 否则直接点「开始」命中有货时不会推送
func TestBarkUrlRestoredFromSettings(t *testing.T) {
	isolateSettings(t)

	const wantUrl = "https://api.day.app/restored-key"

	if err := services.SaveSettings(services.UserSettings{
		SelectedArea:  "中国大陆",
		BarkNotifyUrl: wantUrl,
	}); err != nil {
		t.Fatal(err)
	}

	services.Listen.SetBarkNotifyUrl("")

	areaWidget, storeWidget, productWidget, barkWidget := newTestWidgets()
	loadUserSettingsCache(areaWidget, storeWidget, productWidget, barkWidget, newNotifyWidget(), newIntervalWidget(), newKeepGoingWidget())

	if got := barkWidget.Text; got != wantUrl {
		t.Errorf("输入框未恢复\n期望: %s\n实际: %s", wantUrl, got)
	}
	if got := services.Listen.GetBarkNotifyUrl(); got != wantUrl {
		t.Errorf("监听服务未拿到恢复的 Bark 地址\n期望: %s\n实际: %s", wantUrl, got)
	}
}

// 用户改动输入框后，即使不点「添加」，监听服务也应使用新地址
func TestBarkUrlSyncsOnEdit(t *testing.T) {
	services.Listen.SetBarkNotifyUrl("")

	barkWidget := newBarkWidget()

	barkWidget.SetText("https://api.day.app/first")
	if got := services.Listen.GetBarkNotifyUrl(); got != "https://api.day.app/first" {
		t.Fatalf("首次输入未同步，实际: %q", got)
	}

	barkWidget.SetText("https://api.day.app/second")
	if got := services.Listen.GetBarkNotifyUrl(); got != "https://api.day.app/second" {
		t.Fatalf("修改后未同步，实际: %q", got)
	}

	barkWidget.SetText("")
	if got := services.Listen.GetBarkNotifyUrl(); got != "" {
		t.Fatalf("清空后未同步，实际: %q", got)
	}
}

// 没有缓存文件时走默认分支，不应崩溃，也不应残留 Bark 地址
func TestLoadSettingsWithoutCacheFile(t *testing.T) {
	isolateSettings(t)

	services.Listen.SetBarkNotifyUrl("")

	areaWidget, storeWidget, productWidget, barkWidget := newTestWidgets()
	loadUserSettingsCache(areaWidget, storeWidget, productWidget, barkWidget, newNotifyWidget(), newIntervalWidget(), newKeepGoingWidget())

	if got := services.Listen.GetBarkNotifyUrl(); got != "" {
		t.Errorf("无缓存时 Bark 地址应为空，实际: %q", got)
	}
	if areaWidget.Selected != services.Listen.GetArea().Title {
		t.Errorf("无缓存时应选中默认地区，实际: %q", areaWidget.Selected)
	}
}
