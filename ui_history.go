package main

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

// showHistoryDialog 展示有货记录。
//
// 用户盯二十家门店时，真正需要的判断是「该重点盯哪几家」——
// 这个判断只能靠历史命中情况，而程序原本把每一轮结果都丢掉了。
func showHistoryDialog() {
	entries, err := services.LoadHistory()
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}

	if len(entries) == 0 {
		dialog.ShowInformation("有货记录",
			"还没有记录到有货。\n\n开始监听后，每次命中的时间、门店与型号会记在这里。",
			view.Window)
		return
	}

	list := widget.NewList(
		func() int { return len(entries) },
		func() fyne.CanvasObject { return widget.NewLabel("记录") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(entries) {
				return
			}
			e := entries[id]
			obj.(*widget.Label).SetText(fmt.Sprintf("%s　%s　%s", e.Time, e.Store, e.Product))
		},
	)

	content := container.NewBorder(
		widget.NewLabel(historySummary(entries)),
		nil, nil, nil,
		list,
	)
	content.Resize(fyne.NewSize(720, 460))

	custom := dialog.NewCustom("有货记录", "关闭", content, view.Window)
	custom.Resize(fyne.NewSize(760, 520))
	custom.Show()
}

// historySummary 按命中次数列出门店，次数多的排前面
func historySummary(entries []services.HistoryEntry) string {
	counts := services.StoreHitCount(entries)

	type row struct {
		store string
		count int
	}

	rows := make([]row, 0, len(counts))
	for store, count := range counts {
		rows = append(rows, row{store, count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].store < rows[j].store
	})

	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 次命中，涉及 %d 家门店\n", len(entries), len(rows))

	// 只列前几家，再多对判断没有帮助
	for i, r := range rows {
		if i >= 5 {
			fmt.Fprintf(&b, "其余 %d 家从略", len(rows)-i)
			break
		}
		fmt.Fprintf(&b, "%s %d 次　", r.store, r.count)
	}

	return strings.TrimRight(b.String(), "　")
}
