package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

// 按钮按「一个函数一个按钮」拆开：左栏要把它们两两排进网格，
// 原先两个函数各自返回一整行 HBox，排版就被按钮的分组方式锁死了。

// newAddButton 把勾选的门店 × 型号批量加入监听。
// 这是左栏的主操作，用高重要性（实心蓝底）与其余按钮区分。
func newAddButton(
	areaWidget *widget.Select,
	storeSelect *multiSelect,
	productSelect *multiSelect,
	barkNotifyWidget *widget.Entry,
) *widget.Button {

	button := widget.NewButtonWithIcon("添加", fynetheme.ContentAddIcon(), func() {
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
	})
	button.Importance = widget.HighImportance

	return button
}

// newCleanButton 清空当前地区的监听列表。
//
// 这个动作不可逆，且此前与「添加」同宽并排 —— 两个同样大小的按钮挨着，
// 一个是日常操作一个会清掉全部配置，误触的代价完全不对等。
// 现在降为低重要性的小按钮，并且必须二次确认。
func newCleanButton() *widget.Button {
	button := widget.NewButton("清空当前地区", func() {
		count := len(services.Listen.CurrentAreaItems())
		if count == 0 {
			dialog.ShowInformation("清空", "当前地区没有监听项", view.Window)
			return
		}

		dialog.ShowConfirm("清空监听列表", cleanConfirmMessage(count), func(ok bool) {
			if !ok {
				return
			}

			services.Listen.Clean()
			if err := services.ClearSettings(); err != nil {
				log.Println("清除配置失败:", err)
			}
		}, view.Window)
	})
	button.Importance = widget.LowImportance

	return button
}

// cleanConfirmMessage 说清将要清掉什么。
// 只问「确定吗」的确认框等于没问 —— 用户不知道自己要失去多少东西。
func cleanConfirmMessage(count int) string {
	return fmt.Sprintf("将清空当前地区的 %d 项监听，此操作不可撤销。\n其他地区的监听列表不受影响。", count)
}

// newTestNotifyButton 向所有已配置的通知地址各发一条，并逐条汇报结果
func newTestNotifyButton() *widget.Button {
	return widget.NewButton("测试通知", func() {
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
	})
}

func newAlertSoundButton() *widget.Button {
	return widget.NewButton("试听提示音", func() {
		go alertMp3()
	})
}

func newHistoryButton() *widget.Button {
	return widget.NewButtonWithIcon("有货记录", fynetheme.ListIcon(), func() {
		showHistoryDialog()
	})
}

func newLogButton() *widget.Button {
	return widget.NewButtonWithIcon("打开日志", fynetheme.FolderOpenIcon(), func() {
		dir, err := services.LogDir()
		if err != nil {
			dialog.ShowError(err, view.Window)
			return
		}

		if err := view.App.OpenURL(&url.URL{Scheme: "file", Path: dir}); err != nil {
			// 打不开就把路径显示出来，至少用户能自己找过去
			dialog.ShowInformation("日志位置", dir, view.Window)
		}
	})
}
