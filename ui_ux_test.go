package main

import (
	"testing"

	"fyne.io/fyne/v2"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

func row(status string) services.ListenRow {
	return services.ListenRow{ListenItem: services.ListenItem{Status: status}}
}

// 几十行时用户只关心「有货 / 未知」，筛选只改展示不影响监听
func TestFilterRows(t *testing.T) {
	rows := []services.ListenRow{
		row(services.StatusInStock),
		row(services.StatusOutStock),
		row(services.StatusUnknown),
		row(services.StatusOutStock),
		row(services.StatusWait),
	}

	cases := map[string]int{
		filterAll:               5,
		services.StatusInStock:  1,
		services.StatusOutStock: 2,
		services.StatusUnknown:  1,
		services.StatusWait:     1,
	}

	for filter, want := range cases {
		if got := len(filterRows(rows, filter)); got != want {
			t.Errorf("筛选 %q 应得 %d 项，实际 %d", filter, want, got)
		}
	}
}

// 筛选不得改动传入的切片，否则原始数据会被就地打乱
func TestFilterRowsDoesNotMutateInput(t *testing.T) {
	rows := []services.ListenRow{
		row(services.StatusInStock),
		row(services.StatusOutStock),
	}

	_ = filterRows(rows, services.StatusInStock)

	if rows[0].Status != services.StatusInStock || rows[1].Status != services.StatusOutStock {
		t.Errorf("原切片被修改: %+v", rows)
	}
}

// 没有配置时用默认尺寸
func TestRestoreWindowSizeDefaults(t *testing.T) {
	isolateSettings(t)

	got := restoreWindowSize()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Errorf("无配置时应为默认尺寸，实际 %v", got)
	}
}

func TestRestoreWindowSizeFromSettings(t *testing.T) {
	isolateSettings(t)

	if err := services.SaveSettings(services.UserSettings{WindowWidth: 1280, WindowHeight: 900}); err != nil {
		t.Fatal(err)
	}

	got := restoreWindowSize()
	if got.Width != 1280 || got.Height != 900 {
		t.Errorf("应恢复保存的尺寸，实际 %v", got)
	}
}

// 过小的尺寸会让界面挤成一团，宁可回落到默认值
func TestRestoreWindowSizeRejectsTooSmall(t *testing.T) {
	isolateSettings(t)

	if err := services.SaveSettings(services.UserSettings{WindowWidth: 120, WindowHeight: 80}); err != nil {
		t.Fatal(err)
	}

	got := restoreWindowSize()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Errorf("过小的尺寸应回落到默认值，实际 %v", got)
	}
}

// 窗口尚未创建时不应 panic
func TestCurrentWindowSizeWithoutWindow(t *testing.T) {
	orig := view.Window
	view.Window = nil
	defer func() { view.Window = orig }()

	if got := currentWindowSize(); got != (fyne.Size{}) {
		t.Errorf("无窗口时应返回零值，实际 %v", got)
	}
}
