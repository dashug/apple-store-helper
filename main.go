package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"apple-store-helper/common"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

// main 主函数 (Main function)
func main() {
	initMP3Player()
	initFyneApp()

	// 默认地区 (Default Area)
	defaultArea := services.Listen.GetArea().Title

	// 门店与型号都支持多选，一次可以把「多个门店 × 多个型号」全部加入监听
	storeSelect := newMultiSelect("搜索门店", 150)
	storeSelect.SetOptions(services.Store.ByAreaTitleForOptions(defaultArea))

	productSelect := newMultiSelect("搜索型号", 150)
	productSelect.SetOptions(services.Product.ByAreaTitleForOptions(defaultArea))

	barkWidget := newBarkWidget()
	intervalWidget := newIntervalWidget()

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

		services.Listen.SetArea(services.Area.GetArea(value))
		services.Listen.Clean()
	})
	areaWidget.Horizontal = true

	listenList, warning, refreshList := newListenList()
	services.Listen.SetOnChange(refreshList)

	help := `1. 在 Apple 官网将需要购买的型号加入购物车
2. 勾选地区、门店与型号（都可多选），点击“添加”批量加入监听列表
3. 点击“开始”开始监听，检测到有货时会自动打开购物车页面
`

	loadUserSettingsCache(areaWidget, storeSelect, productSelect, barkWidget, intervalWidget)
	refreshList()

	form := container.NewVBox(
		widget.NewLabel(help),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择地区:"), areaWidget),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择门店:"), storeSelect.container),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择型号:"), productSelect.container),
		container.New(layout.NewFormLayout(), widget.NewLabel("Bark 通知地址"), barkWidget),
		container.New(layout.NewFormLayout(), widget.NewLabel("监听间隔:"), intervalWidget),

		container.NewBorder(nil, nil,
			createActionButtons(areaWidget, storeSelect, productSelect, barkWidget),
			createControlButtons(),
		),
		warning,
	)

	// 列表放在中间，窗口拉大时由它占满剩余空间
	view.Window.SetContent(container.NewBorder(
		form,
		createVersionLabel(),
		nil, nil,
		listenList,
	))

	view.Window.Resize(fyne.NewSize(1000, 800))
	view.Window.CenterOnScreen()
	services.Listen.Run()
	view.Window.ShowAndRun()
}

// newListenList 构建监听列表。
//
// 返回列表控件、整体告警条，以及刷新函数 —— 刷新函数会被监听 goroutine
// 调用，因此行数据需要加锁保护。
func newListenList() (fyne.CanvasObject, *widget.Label, func()) {
	var mu sync.Mutex
	rows := services.Listen.SortedRows()

	warning := widget.NewLabel("⚠️ 当前无法获取库存，下列结果均不可信（接口可能已变更或被限流）")
	warning.Hide()

	list := widget.NewList(
		func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(rows)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("［状态］"),
				widget.NewLabel("时间"),
				widget.NewLabel("门店 型号"),
				layout.NewSpacer(),
				widget.NewButton("删除", nil),
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

			items := obj.(*fyne.Container).Objects
			items[0].(*widget.Label).SetText("［" + row.Status + "］")
			items[1].(*widget.Label).SetText(row.Time.String())

			info := row.Store.CityStoreName + "  " + row.Product.Title
			if row.Detail != "" {
				info += "  (" + row.Detail + ")"
			}
			items[2].(*widget.Label).SetText(info)

			key := row.Key
			items[4].(*widget.Button).OnTapped = func() {
				services.Listen.Remove(key)
				saveSettings(nil)
			}
		},
	)

	refresh := func() {
		mu.Lock()
		rows = services.Listen.SortedRows()
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

			list.Refresh()
		})
	}

	return list, warning, refresh
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

	if err := services.SaveSettings(current); err != nil {
		log.Println("保存配置失败:", err)
	}
}

// 加载用户设置缓存 (Load user settings cache)
func loadUserSettingsCache(areaWidget *widget.RadioGroup, storeSelect *multiSelect, productSelect *multiSelect, barkNotifyWidget *widget.Entry, intervalWidget *widget.Select) {
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
		widget.NewButton("试听(有货提示音)", func() {
			go services.Listen.AlertMp3()
		}),
		widget.NewButton("测试 Bark 通知", func() {
			services.Listen.SendPushNotificationByBark("有货提醒（测试）", "此为测试提醒，点击通知将跳转到相关链接", "https://www.apple.com.cn/shop/bag")
		}),
	)
}

// 创建控制按钮 (Create control buttons)
func createControlButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("开始", func() {
			services.Listen.Status.Set(services.Running)
		}),
		widget.NewButton("暂停", func() {
			services.Listen.Status.Set(services.Pause)
		}),
		container.NewCenter(widget.NewLabel("状态:")),
		container.NewCenter(widget.NewLabelWithData(services.Listen.Status)),
	)
}

// createVersionLabel 创建版本标签 (Create version label)
func createVersionLabel() *fyne.Container {
	return container.NewHBox(
		layout.NewSpacer(),
		widget.NewLabel("version: "+common.VERSION),
	)
}
