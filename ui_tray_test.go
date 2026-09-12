package main

import (
	"bytes"
	"image/png"
	"testing"

	"fyne.io/fyne/v2"

	"apple-store-helper/view"
)

// 托盘图标必须真的被嵌进二进制，否则托盘会显示成「损坏图片」占位图
func TestTrayIconIsEmbedded(t *testing.T) {
	if len(iconPNG) == 0 {
		t.Fatal("Icon.png 未被嵌入")
	}

	if _, err := png.Decode(bytes.NewReader(iconPNG)); err != nil {
		t.Errorf("嵌入的不是有效 PNG: %v", err)
	}

	if trayIcon.Content() == nil || len(trayIcon.Content()) != len(iconPNG) {
		t.Error("托盘资源内容与嵌入数据不一致")
	}
}

// 托盘不可用时必须返回 false 并保持「关闭即退出」——
// 否则用户关掉窗口后既回不来，也没有退出入口
func TestSetupSystemTrayDeclinesWithoutDesktopApp(t *testing.T) {
	view.App = fyne.CurrentApp()
	view.Window = view.App.NewWindow("tray-test")
	defer view.Window.Close()

	// 测试驱动不是桌面驱动，不实现 desktop.App
	if setupSystemTray() {
		t.Error("非桌面环境不应启用托盘，否则会劫持关闭按钮")
	}
}

func TestShowMainWindowDoesNotPanic(t *testing.T) {
	view.App = fyne.CurrentApp()
	view.Window = view.App.NewWindow("show-test")
	defer view.Window.Close()

	view.Window.Hide()
	showMainWindow()
}
