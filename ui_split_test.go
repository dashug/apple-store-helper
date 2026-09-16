package main

import (
	"testing"

	"apple-store-helper/services"
)

// 老配置文件没有这两个字段，零值必须回落到默认而不是把分隔条拖到最左
func TestSplitRatioFallsBackOnZero(t *testing.T) {
	if got := splitRatioOrDefault(0, defaultSidebarRatio); got != defaultSidebarRatio {
		t.Errorf("零值应回落到默认 %v，实际 %v", defaultSidebarRatio, got)
	}
}

// 越界的值不该被采用：另一侧会窄到没法用，而且这个坏值会一直被存回去
func TestSplitRatioRejectsExtremes(t *testing.T) {
	for _, v := range []float64{-1, 0.01, 0.95, 1, 42} {
		if got := splitRatioOrDefault(v, defaultSidebarRatio); got != defaultSidebarRatio {
			t.Errorf("%v 超出可用范围，应回落到 %v，实际 %v", v, defaultSidebarRatio, got)
		}
	}
}

func TestSplitRatioKeepsUsableValues(t *testing.T) {
	for _, v := range []float64{0.15, 0.3, 0.5, 0.85} {
		if got := splitRatioOrDefault(v, defaultSidebarRatio); got != v {
			t.Errorf("%v 在可用范围内，应原样返回，实际 %v", v, got)
		}
	}
}

// 拖好的位置要能穿过一次「保存 → 读取 → 重建界面」
func TestSplitRatiosSurviveRestart(t *testing.T) {
	isolateSettings(t)

	if err := services.SaveSettings(services.UserSettings{
		SidebarRatio:      0.42,
		SidebarListsRatio: 0.7,
	}); err != nil {
		t.Fatal(err)
	}

	buildUI()

	if mainSplit == nil || sidebarSplit == nil {
		t.Fatal("界面未记录分隔条引用")
	}
	if mainSplit.Offset != 0.42 {
		t.Errorf("左栏分隔条应恢复到 0.42，实际 %v", mainSplit.Offset)
	}
	if sidebarSplit.Offset != 0.7 {
		t.Errorf("门店/型号分隔条应恢复到 0.7，实际 %v", sidebarSplit.Offset)
	}
}

// 保存时要把当前位置写回配置文件
func TestSplitRatiosAreSaved(t *testing.T) {
	isolateSettings(t)

	buildUI()
	mainSplit.SetOffset(0.38)
	sidebarSplit.SetOffset(0.62)

	saveSettings(nil)

	settings, err := services.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SidebarRatio != 0.38 {
		t.Errorf("应保存左栏分隔条位置 0.38，实际 %v", settings.SidebarRatio)
	}
	if settings.SidebarListsRatio != 0.62 {
		t.Errorf("应保存门店/型号分隔条位置 0.62，实际 %v", settings.SidebarListsRatio)
	}
}
