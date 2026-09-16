package main

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"net/url"
	"runtime"
	"strings"
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
	"fyne.io/fyne/v2/layout"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/golang-module/carbon"
)

// main 主函数 (Main function)
func main() {
	initLogging()
	initMP3Player()
	initFyneApp()

	view.Window.SetContent(buildUI())
	view.Window.Resize(restoreWindowSize())
	view.Window.CenterOnScreen()

	// 监控类工具关掉窗口就退出是反直觉的，收进托盘后可以挂一整天
	setupSystemTray()

	services.Listen.Run()
	view.Window.ShowAndRun()
}

// buildUI 组装主界面。
// 拆成独立函数是为了让截图生成器复用同一套界面，避免截图与实际界面脱节。
func buildUI() fyne.CanvasObject {
	// 默认地区 (Default Area)
	defaultArea := services.Listen.GetArea().Title

	// 门店与型号都支持多选，一次可以把「多个门店 × 多个型号」全部加入监听
	storeSelect := newMultiSelect("搜索门店", 150)
	storeSelect.SetOptions(services.Store.ByAreaTitleForOptions(defaultArea))

	productSelect := newMultiSelect("搜索型号", 150)
	productSelect.SetOptions(services.Product.ByAreaTitleForOptions(defaultArea))

	barkWidget := newBarkWidget()
	notifyWidget := newNotifyWidget()
	intervalWidget := newIntervalWidget()
	keepGoingWidget := newKeepGoingWidget()

	// 地区选择器 (Area Selector)
	areaWidget := widget.NewRadioGroup(services.Area.ForOptions(), func(value string) {
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
	areaWidget.Horizontal = true

	listenList, warning, refreshList := newListenList()
	controls, refreshStatus := createControlButtons()

	// 列表与状态栏一起刷新：项数和上轮时间都随监听结果变化
	services.Listen.SetOnChange(func() {
		refreshList()
		fyne.Do(refreshStatus)
	})

	// 命中有货时的弹窗、打开购物袋、提示音由界面层提供
	services.Listen.SetOnInStock(handleInStock)

	help := `1. 在 Apple 官网将需要购买的型号加入购物车
2. 勾选地区、门店与型号（都可多选），点击“添加”批量加入监听列表
3. 点击“开始”开始监听，检测到有货时会自动打开购物车页面
`

	loadUserSettingsCache(areaWidget, storeSelect, productSelect, barkWidget, notifyWidget, intervalWidget, keepGoingWidget)
	refreshList()

	// 五行共用一个 FormLayout，否则每行各自计算标签列宽，右侧控件起始位置会参差不齐
	form := container.NewVBox(
		widget.NewLabel(help),
		container.New(layout.NewFormLayout(),
			widget.NewLabel("选择地区:"), areaWidget,
		),

		// 门店与型号并排，否则两个 150px 的多选框会把监听列表挤到只剩两行
		container.NewGridWithColumns(2,
			container.NewBorder(widget.NewLabel("选择门店（可多选）:"), nil, nil, nil, storeSelect.container),
			container.NewBorder(widget.NewLabel("选择型号（可多选）:"), nil, nil, nil, productSelect.container),
		),

		container.New(layout.NewFormLayout(),
			widget.NewLabel("Bark 通知地址:"), barkWidget,
			widget.NewLabel("其他通知地址:"), notifyWidget,
			widget.NewLabel("监听间隔:"), container.NewBorder(nil, nil, nil, keepGoingWidget, intervalWidget),
		),

		// 主操作与次要操作分两行，避免七个按钮挤在一行、窗口缩小时先挤坏
		container.NewBorder(nil, nil,
			createActionButtons(areaWidget, storeSelect, productSelect, barkWidget),
			controls,
		),
		createSecondaryButtons(),
		warning,
	)

	// 列表放在中间，窗口拉大时由它占满剩余空间
	return container.NewBorder(
		form,
		createVersionLabel(),
		nil, nil,
		listenList,
	)
}

// newListenList 构建监听列表。
//
// 返回列表控件、整体告警条，以及刷新函数 —— 刷新函数会被监听 goroutine
// 调用，因此行数据需要加锁保护。
// filterAll 表示不筛选
const filterAll = "全部"

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
			status := canvas.NewText("［状态］", fynetheme.Color(fynetheme.ColorNameForeground))
			status.TextStyle.Bold = true

			// 时间、门店、型号合并进同一个 Label。
			// 拆成多个 Label 排在 HBox 里时，文字变长后不会重新布局，会互相重叠。
			return container.NewBorder(nil, nil,
				status,
				container.NewHBox(
					widget.NewButton("停用", nil),
					widget.NewButton("删除", nil),
				),
				widget.NewLabel("详情"),
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

			info := items[0].(*widget.Label)
			status := items[1].(*canvas.Text)
			buttons := items[2].(*fyne.Container).Objects
			toggle := buttons[0].(*widget.Button)
			remove := buttons[1].(*widget.Button)

			// 有货用绿色、未知用警示色，否则命中的那条混在几十行里不够显眼。
			// 停用项显示「已停用」而不是旧状态 —— 它不再被查询，旧状态是过期信息。
			display := row.DisplayStatus()
			status.Text = "［" + display + "］"
			status.Color = statusColor(display)
			status.Refresh()

			text := row.Store.CityStoreName + "　" + row.Product.Title
			if row.Detail != "" {
				text += "　(" + row.Detail + ")"
			}
			if !row.Time.IsZero() {
				text += "　" + row.Time.ToTimeString()
			}
			info.SetText(text)

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

			remove.OnTapped = func() {
				services.Listen.Remove(key)
				saveSettings(nil)
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

	panel := container.NewBorder(
		container.NewHBox(widget.NewLabel("筛选:"), filterSelect, filterHint),
		nil, nil, nil,
		list,
	)

	return panel, warning, refresh
}

const (
	defaultWindowWidth  = 1000
	defaultWindowHeight = 800

	// 低于此尺寸界面会挤成一团，恢复成一条缝还不如用默认值
	minWindowWidth  = 720
	minWindowHeight = 540
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
	view.Window = view.App.NewWindow("Apple Store Helper")
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
func loadUserSettingsCache(areaWidget *widget.RadioGroup, storeSelect *multiSelect, productSelect *multiSelect, barkNotifyWidget *widget.Entry, notifyWidget *widget.Entry, intervalWidget *widget.Select, keepGoingWidget *widget.Check) {
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

// 创建动作按钮 (Create action buttons)
func createActionButtons(areaWidget *widget.RadioGroup, storeSelect *multiSelect, productSelect *multiSelect, barkNotifyWidget *widget.Entry) *fyne.Container {
	return container.NewHBox(
		widget.NewButton("添加", func() {
			stores := storeSelect.Selected()
			products := productSelect.Selected()

			if len(stores) == 0 || len(products) == 0 {
				dialog.ShowError(errors.New("请至少勾选一个门店和一个型号"), view.Window)
				return
			}

			added, err := services.Listen.AddMany(areaWidget.Selected, stores, products)
			if err != nil {
				dialog.ShowError(err, view.Window)
				return
			}

			saveSettings(&services.UserSettings{
				SelectedArea:    areaWidget.Selected,
				SelectedStore:   stores[0],
				SelectedProduct: products[0],
				BarkNotifyUrl:   barkNotifyWidget.Text,
			})

			// 清空勾选：否则那些勾还留在界面上，不知道算不算数
			storeSelect.ClearSelection()
			productSelect.ClearSelection()

			skipped := len(stores)*len(products) - added
			msg := fmt.Sprintf("已添加 %d 项", added)
			if skipped > 0 {
				msg += fmt.Sprintf("，%d 项已在监听中", skipped)
			}
			dialog.ShowInformation("添加完成", msg, view.Window)
		}),
		widget.NewButton("清空", func() {
			services.Listen.Clean()
			if err := services.ClearSettings(); err != nil {
				log.Println("清除配置失败:", err)
			}
		}),
	)
}

// createSecondaryButtons 次要操作。
// 与「添加/清空/开始/暂停」分开，主行不至于挤到窗口一缩小就排不下。
func createSecondaryButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("试听提示音", func() {
			go alertMp3()
		}),
		widget.NewButton("测试通知", func() {
			if len(services.Listen.NotifyTargets()) == 0 {
				dialog.ShowInformation("测试通知", "尚未配置任何通知地址", view.Window)
				return
			}

			// 放到后台发送，逐条汇报结果 ——
			// 原先点了没有任何反馈，配错地址要到真正命中有货时才会发现
			go func() {
				results := services.Listen.Notify(services.Notification{
					Title:   "有货提醒（测试）",
					Content: "此为测试提醒，点击通知将跳转到相关链接",
					URL:     "https://www.apple.com.cn/shop/bag",
				})

				var report strings.Builder
				for _, r := range results {
					if r.Err != nil {
						fmt.Fprintf(&report, "✗ %s：%v\n", r.Channel, r.Err)
					} else {
						fmt.Fprintf(&report, "✓ %s：已发送\n", r.Channel)
					}
				}

				fyne.Do(func() {
					dialog.ShowInformation("测试通知结果", report.String(), view.Window)
				})
			}()
		}),
		widget.NewButton("有货记录", func() {
			showHistoryDialog()
		}),
		widget.NewButton("打开日志", func() {
			dir, err := services.LogDir()
			if err != nil {
				dialog.ShowError(err, view.Window)
				return
			}

			if err := view.App.OpenURL(&url.URL{Scheme: "file", Path: dir}); err != nil {
				// 打不开就把路径显示出来，至少用户能自己找过去
				dialog.ShowInformation("日志位置", dir, view.Window)
			}
		}),
		layout.NewSpacer(),
	)
}

// createControlButtons 返回控制按钮与状态显示，以及状态刷新函数。
//
// 状态此前只有「暂停 / 监听中」，看不出程序是否还在正常轮转 ——
// 用户只能盯着列表里的时间列变化来判断。现在直接给出项数与上轮完成时间。
func createControlButtons() (*fyne.Container, func()) {
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

	// 倒计时要自己走，不能只等监听结果来触发刷新：
	// 退避时两轮之间可能隔几分钟，那期间状态栏一个字都不会变
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			fyne.Do(update)
		}
	}()

	return container.NewHBox(
		widget.NewButton("开始", func() {
			services.Listen.SetStatus(services.Running)
		}),
		widget.NewButton("暂停", func() {
			services.Listen.SetStatus(services.Pause)
		}),
		container.NewCenter(widget.NewLabel("状态:")),
		container.NewCenter(statusLabel),
	), update
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

// createVersionLabel 创建版本标签 (Create version label)
func createVersionLabel() *fyne.Container {
	return container.NewHBox(
		layout.NewSpacer(),
		widget.NewLabel("version: "+common.VERSION),
	)
}
