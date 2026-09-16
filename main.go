package main

import (
	"fmt"
	"image/color"
	"log"
	"runtime"
	"sync"
	"time"

	"apple-store-helper/common"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

// main 主函数 (Main function)
func main() {
	initLogging()
	initMP3Player()
	initFyneApp()

	app := buildUI()
	view.Window.SetContent(app.content)
	view.Window.Resize(restoreWindowSize())
	view.Window.CenterOnScreen()

	// 监控类工具关掉窗口就退出是反直觉的，收进托盘后可以挂一整天
	setupSystemTray()

	services.Listen.Run()
	app.startStatusTicker()
	view.Window.ShowAndRun()
}

// newListenList 构建监听列表。
//
// 返回列表控件、整体告警条，以及刷新函数 —— 刷新函数会被监听 goroutine
// 调用，因此行数据需要加锁保护。
// filterAll 表示不筛选
const filterAll = "全部"

// filterSelectWidth 固定筛选下拉的宽度：放进表头会被拉伸得很宽，
// 而它的选项最长也就三个字
const filterSelectWidth = 120

// filterRows 只影响展示，不影响监听范围 ——
// 几十行时用户只关心「有货 / 未知」这两类，其余是噪声
func filterRows(rows []services.ListenRow, filter string) []services.ListenRow {
	if filter == filterAll {
		return rows
	}

	out := make([]services.ListenRow, 0, len(rows))
	for _, row := range rows {
		if row.DisplayStatus() == filter {
			out = append(out, row)
		}
	}

	return out
}

func newListenList() (fyne.CanvasObject, *widget.Label, func()) {
	var (
		mu     sync.Mutex
		filter = filterAll
	)
	rows := services.Listen.SortedRows()

	warning := widget.NewLabel("⚠️ 当前无法获取库存，下列结果均不可信（接口可能已变更或被限流）")
	warning.Hide()

	filterSelect := widget.NewSelect(
		[]string{
			filterAll,
			services.StatusInStock,
			services.StatusUnknown,
			services.StatusOutStock,
			services.StatusWait,
			services.StatusDisabled,
		},
		nil,
	)
	filterSelect.Selected = filterAll

	filterHint := widget.NewLabel("")

	list := widget.NewList(
		func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(rows)
		},
		func() fyne.CanvasObject {
			status := canvas.NewText("● 状态", fynetheme.Color(fynetheme.ColorNameForeground))
			status.TextStyle.Bold = true

			// 门店一行、型号与详情一行。横向并排的多个 Label 在文字变长后
			// 不会重新布局、会互相重叠，纵向堆叠没有这个问题；
			// 两行都开省略号截断，再长也只是截断，不会压到旁边的按钮上。
			store := widget.NewLabel("门店")
			store.TextStyle.Bold = true
			store.Truncation = fyne.TextTruncateEllipsis

			detail := widget.NewLabel("型号")
			detail.Importance = widget.LowImportance
			detail.SizeName = fynetheme.SizeNameCaptionText
			detail.Truncation = fyne.TextTruncateEllipsis

			// 删除不可逆，停用只是暂停查询 —— 两者此前是同宽同色的相邻按钮，
			// 手一抖丢的是配置。现在删除是一个低调的图标按钮，与停用之间
			// 隔一条分隔线，真正的防护是点下去之后的确认框。
			//
			// 没有用 DangerImportance：那会渲染成实心红块，几十行列表里
			// 满屏红色，最该被看见的「有货」反而被盖过去了。
			remove := widget.NewButtonWithIcon("", fynetheme.DeleteIcon(), nil)
			remove.Importance = widget.LowImportance

			// 按钮套一层 Center：直接放进 Border 的右侧会被拉伸到整行高，
			// 两行式的行本来就高，拉伸后整行全是按钮
			return container.NewBorder(nil, nil,
				container.NewCenter(status),
				container.NewCenter(container.NewHBox(
					widget.NewButton("停用", nil),
					widget.NewSeparator(),
					remove,
				)),
				container.NewVBox(store, detail),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			mu.Lock()
			if id < 0 || id >= len(rows) {
				mu.Unlock()
				return
			}
			row := rows[id]
			mu.Unlock()

			// Border 的 Objects 顺序为 [center, left, right]
			items := obj.(*fyne.Container).Objects

			lines := items[0].(*fyne.Container).Objects
			store := lines[0].(*widget.Label)
			detail := lines[1].(*widget.Label)

			// 左右两侧都套了一层 Center，取内容要多剥一层
			status := items[1].(*fyne.Container).Objects[0].(*canvas.Text)
			buttons := items[2].(*fyne.Container).Objects[0].(*fyne.Container).Objects
			toggle := buttons[0].(*widget.Button)
			remove := buttons[2].(*widget.Button) // buttons[1] 是分隔条

			// 有货用绿色、未知用警示色，否则命中的那条混在几十行里不够显眼。
			// 停用项显示「已停用」而不是旧状态 —— 它不再被查询，旧状态是过期信息。
			display := row.DisplayStatus()
			status.Text = "● " + display
			status.Color = statusColor(display)
			status.Refresh()

			store.SetText(row.Store.CityStoreName)

			text := row.Product.Title
			if row.Detail != "" {
				text += "　" + row.Detail
			}
			if !row.Time.IsZero() {
				text += "　" + row.Time.ToTimeString()
			}
			detail.SetText(text)

			key := row.Key
			disabled := row.Disabled

			if disabled {
				toggle.SetText("启用")
			} else {
				toggle.SetText("停用")
			}
			toggle.OnTapped = func() {
				services.Listen.SetDisabled(key, !disabled)
				saveSettings(nil)
			}

			name := row.Store.CityStoreName + " " + row.Product.Title
			remove.OnTapped = func() {
				dialog.ShowConfirm("删除监听项", "确定删除「"+name+"」？", func(ok bool) {
					if !ok {
						return
					}

					services.Listen.Remove(key)
					saveSettings(nil)
				}, view.Window)
			}
		},
	)

	refresh := func() {
		all := services.Listen.SortedRows()

		mu.Lock()
		rows = filterRows(all, filter)
		shown, total := len(rows), len(all)
		mu.Unlock()

		unreliable := services.Listen.AllUnknown()

		// 这个回调会被监听 goroutine 调用，图形操作必须回到主运行时上下文。
		// fyne.Do 在主线程上调用同样安全：应用尚未启动时直接执行，
		// 启动后则排入主循环队列（队列无界，不会阻塞）。
		fyne.Do(func() {
			if unreliable {
				warning.Show()
			} else {
				warning.Hide()
			}

			// 筛选掉多少要说清楚，否则用户会以为监听项丢了
			if shown == total {
				filterHint.SetText("")
			} else {
				filterHint.SetText(fmt.Sprintf("（%d / %d 项）", shown, total))
			}

			list.Refresh()
		})
	}

	filterSelect.OnChanged = func(value string) {
		mu.Lock()
		filter = value
		mu.Unlock()

		refresh()
	}

	// 右侧内容区的表头：左边标题，右边筛选，中间一条分隔线与列表分开
	title := widget.NewLabel("监听列表")
	title.TextStyle.Bold = true

	header := container.NewBorder(nil, nil,
		title,
		container.NewHBox(filterHint, container.NewGridWrap(
			fyne.NewSize(filterSelectWidth, filterSelect.MinSize().Height), filterSelect)),
	)

	panel := container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()),
		nil, nil, nil,
		list,
	)

	return panel, warning, refresh
}

const (
	defaultWindowWidth  = 1000
	defaultWindowHeight = 800

	// 低于此尺寸界面会挤成一团，恢复成一条缝还不如用默认值。
	//
	// 高度略高于旧版：左栏里有地区、两个多选列表和两行按钮。
	// TestUIFitsMinimumWindow 盯着这条线 —— 谁再往界面上加一个固定高度的
	// 控件，测试会先红。设置搬进对话框后这里从 620 降回 580。
	minWindowWidth  = 720
	minWindowHeight = 580
)

// restoreWindowSize 读取上次的窗口尺寸，缺失或过小时回落到默认值
func restoreWindowSize() fyne.Size {
	settings, err := services.LoadSettings()
	if err != nil {
		return fyne.NewSize(defaultWindowWidth, defaultWindowHeight)
	}

	width, height := float32(settings.WindowWidth), float32(settings.WindowHeight)
	if width < minWindowWidth || height < minWindowHeight {
		return fyne.NewSize(defaultWindowWidth, defaultWindowHeight)
	}

	return fyne.NewSize(width, height)
}

// currentWindowSize 返回当前窗口尺寸，窗口尚未创建时返回零值
func currentWindowSize() fyne.Size {
	if view.Window == nil {
		return fyne.Size{}
	}

	return view.Window.Canvas().Size()
}

// statusColor 返回状态对应的颜色，取自主题以便跟随明暗模式
func statusColor(status string) color.Color {
	switch status {
	case services.StatusInStock:
		return fynetheme.Color(fynetheme.ColorNameSuccess)
	case services.StatusUnknown:
		return fynetheme.Color(fynetheme.ColorNameWarning)
	case services.StatusOutStock, services.StatusDisabled:
		return fynetheme.Color(fynetheme.ColorNameDisabled)
	default:
		return fynetheme.Color(fynetheme.ColorNameForeground)
	}
}

// initLogging 让日志落盘。
// 双击启动 .app 时 stdout 不指向任何用户能看到的地方，不落盘等于没有日志。
func initLogging() {
	path, err := services.SetupLogging()
	if err != nil {
		log.Println("日志文件初始化失败:", err)
		return
	}

	log.Printf("Apple Store Helper %s 启动 (%s/%s)，日志: %s",
		common.VERSION, runtime.GOOS, runtime.GOARCH, path)
}

// initMP3Player 初始化 MP3 播放器 (Initialize MP3 player)
func initMP3Player() {
	SampleRate := beep.SampleRate(44100)
	speaker.Init(SampleRate, SampleRate.N(time.Second/10))
}

// initFyneApp 初始化 Fyne 应用 (Initialize Fyne App)
func initFyneApp() {
	view.App = app.NewWithID("apple-store-helper")
	view.App.Settings().SetTheme(&theme.MyTheme{})
	view.Window = view.App.NewWindow(appName)
}

// intervalOptions 轮询间隔的可选项。
//
// 间隔越短，有货后越早发现；但请求越密也越容易被 Apple 限流，而限流的
// 结果是全部显示「未知」—— 恰好在最需要结果的时刻拿不到结果。
var intervalOptions = []struct {
	label   string
	seconds int
}{
	{"2 秒（激进）", 2},
	{"5 秒（推荐）", 5},
	{"10 秒", 10},
	{"30 秒（保守）", 30},
}

func intervalLabels() []string {
	labels := make([]string, 0, len(intervalOptions))
	for _, opt := range intervalOptions {
		labels = append(labels, opt.label)
	}
	return labels
}

func secondsFromLabel(label string) int {
	for _, opt := range intervalOptions {
		if opt.label == label {
			return opt.seconds
		}
	}
	return int(services.DefaultInterval / time.Second)
}

func labelFromSeconds(seconds int) string {
	for _, opt := range intervalOptions {
		if opt.seconds == seconds {
			return opt.label
		}
	}
	return labelFromSeconds(int(services.DefaultInterval / time.Second))
}

// newIntervalWidget 创建轮询间隔选择器。
//
// 回调在设置默认值之后才挂上：否则构造控件本身就会触发一次保存，
// 让「尚未保存过配置」的状态无法成立。
func newIntervalWidget() *widget.Select {
	intervalWidget := widget.NewSelect(intervalLabels(), nil)
	intervalWidget.SetSelected(labelFromSeconds(int(services.DefaultInterval / time.Second)))

	intervalWidget.OnChanged = func(label string) {
		services.Listen.SetInterval(time.Duration(secondsFromLabel(label)) * time.Second)
		saveSettings(nil)
	}

	return intervalWidget
}

// newNotifyWidget 创建其他通知渠道的地址输入框。
//
// Bark 只覆盖 iOS。这里按地址自动识别 Server酱、企业微信、Telegram，
// 其余一律按通用 Webhook（POST 一个 JSON）处理。
func newNotifyWidget() *widget.Entry {
	notifyWidget := widget.NewMultiLineEntry()
	notifyWidget.SetPlaceHolder("每行一个，支持 Server酱 / 企业微信 / Telegram / 通用 Webhook")
	notifyWidget.SetMinRowsVisible(3)
	notifyWidget.OnChanged = services.Listen.SetNotifyUrls

	return notifyWidget
}

// newKeepGoingWidget 创建「命中后继续监听」开关。
//
// 默认命中即暂停（与此前一致）。但盯二十项时，一项命中就全停、其余十九项
// 也不再监控 —— 命中的那家未必是用户去得了的。
func newKeepGoingWidget() *widget.Check {
	keepGoing := widget.NewCheck("命中后继续监听其余项", nil)
	keepGoing.SetChecked(false)

	keepGoing.OnChanged = func(checked bool) {
		services.Listen.SetStopOnHit(!checked)
		saveSettings(nil)
	}

	return keepGoing
}

// newBarkWidget 创建 Bark 地址输入框
// OnChanged 是 Bark 地址的唯一写入源：无论用户手动输入，还是 loadUserSettingsCache
// 通过 SetText 恢复缓存，监听服务持有的地址都会同步更新
func newBarkWidget() *widget.Entry {
	barkWidget := widget.NewEntry()
	barkWidget.SetPlaceHolder("https://api.day.app/你的BarkKey")
	barkWidget.OnChanged = services.Listen.SetBarkNotifyUrl

	return barkWidget
}

// saveSettings 保存当前配置。传入 nil 表示沿用已保存的界面选择。
func saveSettings(settings *services.UserSettings) {
	current, _ := services.LoadSettings()
	if settings != nil {
		current = *settings
	}
	current.ListenItems = services.Listen.GetListenItems()
	current.PollIntervalSeconds = int(services.Listen.GetInterval() / time.Second)
	current.NotifyUrls = services.Listen.GetNotifyUrls()
	current.KeepGoingOnHit = !services.Listen.GetStopOnHit()

	// 过小的尺寸不记，避免把异常状态存下来
	if size := currentWindowSize(); size.Width >= minWindowWidth && size.Height >= minWindowHeight {
		current.WindowWidth = int(size.Width)
		current.WindowHeight = int(size.Height)
	}

	if err := services.SaveSettings(current); err != nil {
		log.Println("保存配置失败:", err)
	}
}

// 加载用户设置缓存 (Load user settings cache)
func loadUserSettingsCache(areaWidget *widget.Select, storeSelect *multiSelect, productSelect *multiSelect, barkNotifyWidget *widget.Entry, notifyWidget *widget.Entry, intervalWidget *widget.Select, keepGoingWidget *widget.Check) {
	settings, err := services.LoadSettings()
	if err != nil {
		areaWidget.SetSelected(services.Listen.GetArea().Title)
		return
	}

	areaWidget.SetSelected(settings.SelectedArea)
	storeSelect.Select(settings.SelectedStore)
	productSelect.Select(settings.SelectedProduct)
	services.Listen.SetListenItems(settings.ListenItems)
	barkNotifyWidget.SetText(settings.BarkNotifyUrl)
	notifyWidget.SetText(settings.NotifyUrls)

	services.Listen.SetStopOnHit(!settings.KeepGoingOnHit)
	keepGoingWidget.Checked = settings.KeepGoingOnHit
	keepGoingWidget.Refresh()

	// 旧配置文件没有这个字段，此时保持默认间隔。
	// 直接赋值而不用 SetSelected，避免恢复配置的动作反过来触发一次保存。
	if settings.PollIntervalSeconds > 0 {
		services.Listen.SetInterval(time.Duration(settings.PollIntervalSeconds) * time.Second)
		intervalWidget.Selected = labelFromSeconds(settings.PollIntervalSeconds)
		intervalWidget.Refresh()
	}
}
