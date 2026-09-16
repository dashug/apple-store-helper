package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/golang-module/carbon"

	"apple-store-helper/common"
	"apple-store-helper/services"
)

// appName 是界面上显示的软件名。
//
// 原名「Apple Store Helper」用的是苹果的商标，本项目与 Apple 无关，
// 顶着它既不准确也不必要。
const appName = "取货雷达"

const (
	// sidebarRatio 是左栏初始占比。用户可以拖动分隔条改，会话内有效。
	sidebarRatio = 0.3

	// sidebarListMinHeight 只是门店/型号列表的下限。
	// 它们的实际高度由左栏的纵向分隔条分配，窗口拉高时跟着长 ——
	// 原先写死 150pt，窗口再大列表也还是那么高。
	sidebarListMinHeight = 70
)

// buildUI 组装主界面。
//
// 形状取自 macOS 系统设置：左栏放配置，右侧整片留给监听列表，顶部工具栏
// 放开始/暂停，底部一条状态栏。原先所有控件挤在上方一个五行表单里，
// 窗口拉大时列表并不会跟着变高 —— 而盯十几家店时最需要的恰恰是列表高度。
//
// 拆成独立函数是为了让截图生成器复用同一套界面，避免截图与实际界面脱节。
func buildUI() ui {
	defaultArea := services.Listen.GetArea().Title

	// 门店与型号都支持多选，一次可以把「多个门店 × 多个型号」全部加入监听
	storeSelect := newMultiSelect("搜索门店", sidebarListMinHeight)
	storeSelect.SetOptions(services.Store.ByAreaTitleForOptions(defaultArea))

	productSelect := newMultiSelect("搜索型号", sidebarListMinHeight)
	productSelect.SetOptions(services.Product.ByAreaTitleForOptions(defaultArea))

	barkWidget := newBarkWidget()
	notifyWidget := newNotifyWidget()
	intervalWidget := newIntervalWidget()
	keepGoingWidget := newKeepGoingWidget()

	// 地区改用下拉框：七个地区排成一行单选钮占的宽度，左栏放不下
	areaWidget := widget.NewSelect(services.Area.ForOptions(), func(value string) {
		// 防止空值或无效值导致崩溃
		if value == "" {
			return
		}

		storeSelect.SetOptions(services.Store.ByAreaTitleForOptions(value))
		storeSelect.ClearSelection()

		productSelect.SetOptions(services.Product.ByAreaTitleForOptions(value))
		productSelect.ClearSelection()

		// 只切换地区，不再清空监听列表 ——
		// 各地区的列表分开保存，切回来即可恢复
		services.Listen.SetArea(services.Area.GetArea(value))
	})

	listenList, warning, refreshList := newListenList()
	statusBar, refreshStatus := newStatusBar()

	// 列表与状态栏一起刷新：项数和上轮时间都随监听结果变化
	services.Listen.SetOnChange(func() {
		refreshList()
		fyne.Do(refreshStatus)
	})

	// 命中有货时的弹窗、打开购物袋、提示音由界面层提供
	services.Listen.SetOnInStock(handleInStock)

	loadUserSettingsCache(areaWidget, storeSelect, productSelect, barkWidget, notifyWidget, intervalWidget, keepGoingWidget)
	refreshList()

	sidebar := newSidebar(areaWidget, storeSelect, productSelect,
		barkWidget, notifyWidget, intervalWidget, keepGoingWidget)

	split := container.NewHSplit(sidebar, container.NewBorder(warning, nil, nil, nil, listenList))
	split.SetOffset(sidebarRatio)

	return ui{
		content:       container.NewBorder(newToolbar(), statusBar, nil, nil, split),
		refreshStatus: refreshStatus,
	}
}

// ui 是组装好的主界面。
type ui struct {
	content fyne.CanvasObject

	// refreshStatus 刷新底部状态栏
	refreshStatus func()
}

// startStatusTicker 每秒刷新一次状态栏，让倒计时自己走动。
//
// 这件事必须由 main 在应用真正跑起来之后启动，不能放进 buildUI ——
// 截图生成器和测试也会调 buildUI，在那里起一个永不结束的 goroutine
// 既会泄漏，也会和调用方并发读写控件：fyne 的测试驱动里 fyne.Do 是
// 就地同步执行的，并不会排进主循环。CI 的 -race 抓到过这个。
func (u ui) startStatusTicker() {
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			fyne.Do(u.refreshStatus)
		}
	}()
}

// newToolbar 是顶部工具栏：左边软件名，右边开始/暂停。
//
// 开始是主操作，用高重要性（实心蓝底）—— macOS 里一屏只有一个这样的按钮。
func newToolbar() fyne.CanvasObject {
	title := canvas.NewText(appName, fynetheme.Color(fynetheme.ColorNameForeground))
	title.TextSize = fynetheme.Size(fynetheme.SizeNameSubHeadingText)
	title.TextStyle.Bold = true

	start := widget.NewButtonWithIcon("开始", fynetheme.MediaPlayIcon(), func() {
		services.Listen.SetStatus(services.Running)
	})
	start.Importance = widget.HighImportance

	pause := widget.NewButtonWithIcon("暂停", fynetheme.MediaPauseIcon(), func() {
		services.Listen.SetStatus(services.Pause)
	})

	bar := container.NewBorder(nil, nil,
		container.NewCenter(title),
		container.NewHBox(start, pause),
	)

	return container.NewVBox(container.NewPadded(bar), widget.NewSeparator())
}

// newSidebar 是左栏：地区、门店、型号、添加，以及折叠起来的通知设置。
//
// 门店与型号用纵向分隔条分配剩余空间 —— 两个列表谁更需要高度因人而异，
// 盯一个型号十家店和盯十个型号一家店是两种用法。
func newSidebar(
	areaWidget *widget.Select,
	storeSelect *multiSelect,
	productSelect *multiSelect,
	barkWidget *widget.Entry,
	notifyWidget *widget.Entry,
	intervalWidget *widget.Select,
	keepGoingWidget *widget.Check,
) fyne.CanvasObject {

	lists := container.NewVSplit(
		sidebarSection("门店（可多选）", storeSelect.container),
		sidebarSection("型号（可多选）", productSelect.container),
	)
	lists.SetOffset(0.5)

	add := newAddButton(areaWidget, storeSelect, productSelect, barkWidget)
	clean := newCleanButton()

	settings := widget.NewAccordion(widget.NewAccordionItem("通知与设置",
		container.NewVBox(
			sidebarSection("Bark 通知地址", barkWidget),
			sidebarSection("其他通知地址（每行一个）", notifyWidget),
			sidebarSection("监听间隔", intervalWidget),
			keepGoingWidget,
			container.NewGridWithColumns(2, newTestNotifyButton(), newAlertSoundButton()),
		),
	))

	top := sidebarSection("地区", areaWidget)

	bottom := container.NewVBox(
		container.NewGridWithColumns(2, add, clean),
		settings,
		container.NewGridWithColumns(2, newHistoryButton(), newLogButton()),
	)

	return container.NewPadded(container.NewBorder(top, bottom, nil, nil, lists))
}

// sidebarSection 给一段配置加上小标题，对应 macOS 侧边栏里的分组标签
func sidebarSection(title string, content fyne.CanvasObject) fyne.CanvasObject {
	label := widget.NewLabel(title)
	label.TextStyle.Bold = true
	label.SizeName = fynetheme.SizeNameCaptionText
	label.Importance = widget.LowImportance

	return container.NewBorder(label, nil, nil, nil, content)
}

// newStatusBar 是底部状态栏：左边运行状态，右边版本号。
func newStatusBar() (fyne.CanvasObject, func()) {
	statusLabel := widget.NewLabel("")

	update := func() {
		statusLabel.SetText(statusText(
			services.Listen.GetStatus(),
			services.Listen.ActiveCount(),
			services.Listen.DisabledCount(),
			services.Listen.LastCheck(),
			services.Listen.NextCheck(),
		))
	}
	update()

	// 注意：这里不起每秒刷新的 goroutine，由 ui.startStatusTicker 负责。
	// 倒计时确实需要自己走（退避时两轮之间可能隔几分钟，
	// 那期间状态栏一个字都不会变），但启动时机不在装配阶段。
	version := widget.NewLabel(common.VERSION)
	version.Importance = widget.LowImportance
	version.SizeName = fynetheme.SizeNameCaptionText

	bar := container.NewBorder(nil, nil, statusLabel, version)

	return container.NewVBox(widget.NewSeparator(), bar), update
}

// statusText 拼出状态栏文本。
//
// 做成纯函数是为了能直接断言：这行字是用户判断「程序还在不在转」的唯一依据，
// 出错不会崩，只会安静地误导人。
func statusText(status string, active, disabled int, last carbon.DateTime, next time.Time) string {
	text := fmt.Sprintf("%s · %d 项", status, active)

	if disabled > 0 {
		text += fmt.Sprintf("（%d 已停用）", disabled)
	}

	if !last.IsZero() {
		text += " · 上轮 " + last.ToTimeString()
	}

	// 暂停时没有下一轮，显示倒计时只会误导
	if status == services.Running && !next.IsZero() {
		if left := formatCountdown(time.Until(next)); left != "" {
			text += " · 下一轮 " + left
		}
	}

	return text
}

// formatCountdown 把剩余时间写成中文短串，已到点则返回空串
func formatCountdown(left time.Duration) string {
	if left <= 0 {
		return ""
	}

	// 向上取整：还剩 0.3 秒时显示「1 秒」比显示「0 秒」诚实
	secs := int((left + time.Second - 1) / time.Second)

	if secs < 60 {
		return fmt.Sprintf("%d 秒", secs)
	}

	return fmt.Sprintf("%d 分 %02d 秒", secs/60, secs%60)
}
