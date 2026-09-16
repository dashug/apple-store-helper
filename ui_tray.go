package main

import (
	_ "embed"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

//go:embed Icon.png
var iconPNG []byte

var trayIcon = fyne.NewStaticResource("icon.png", iconPNG)

// setupSystemTray 把应用放进系统托盘，并让关闭窗口只是收起而非退出。
//
// 返回是否启用成功。非桌面平台、或托盘不可用时返回 false —— 这种情况下
// 必须保留「关闭即退出」，否则用户会失去退出程序的唯一入口。
func setupSystemTray() bool {
	desk, ok := view.App.(desktop.App)
	if !ok {
		return false
	}

	desk.SetSystemTrayIcon(trayIcon)

	// fyne 会自动补上「退出」项，这里不重复添加
	desk.SetSystemTrayMenu(fyne.NewMenu(appName,
		fyne.NewMenuItem("显示主窗口", showMainWindow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("开始监听", func() {
			services.Listen.SetStatus(services.Running)
		}),
		fyne.NewMenuItem("暂停监听", func() {
			services.Listen.SetStatus(services.Pause)
		}),
	))

	var notifyOnce sync.Once
	view.Window.SetCloseIntercept(func() {
		// 收进托盘是最常见的「用完了」动作，顺手记下窗口尺寸
		saveSettings(nil)

		view.Window.Hide()

		// 只提示一次。不提示的话，用户会以为已经退出，
		// 程序却还在后台持续请求 Apple。
		notifyOnce.Do(func() {
			view.App.SendNotification(&fyne.Notification{
				Title:   "仍在后台监听",
				Content: "窗口已收进托盘。点击托盘图标可重新打开，完全退出请用托盘菜单中的退出。",
			})
		})
	})

	return true
}

// showMainWindow 重新显示主窗口并置前
func showMainWindow() {
	view.Window.Show()
	view.Window.RequestFocus()
}
