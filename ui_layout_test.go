package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// buttonLabels 递归收集界面里所有按钮的文字
func buttonLabels(obj fyne.CanvasObject) []string {
	var found []string

	switch o := obj.(type) {
	case *widget.Button:
		found = append(found, o.Text)
	case *fyne.Container:
		for _, child := range o.Objects {
			found = append(found, buttonLabels(child)...)
		}
	case *widget.Accordion:
		for _, item := range o.Items {
			found = append(found, buttonLabels(item.Detail)...)
		}
	case *container.Scroll:
		found = append(found, buttonLabels(o.Content)...)
	case fyne.Widget:
		// Split 等复合控件没有导出的子对象，通过渲染器取
		for _, child := range o.CreateRenderer().Objects() {
			found = append(found, buttonLabels(child)...)
		}
	}

	return found
}

// 界面大改最容易丢的就是某个按钮。这里逐个点名，
// 少一个就红 —— 靠肉眼看截图是发现不了「少了个打开日志」的。
func TestAllButtonsSurviveLayout(t *testing.T) {
	isolateSettings(t)

	labels := buttonLabels(buildUI())
	joined := strings.Join(labels, " ")

	for _, want := range []string{
		"开始", "暂停", // 工具栏
		"添加", "清空", // 左栏主操作
		"测试通知", "试听提示音", // 折叠的通知设置
		"有货记录", "打开日志", // 左栏底部
		"全选", "全不选", // 门店/型号多选
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("界面上找不到「%s」按钮，实际有：%s", want, joined)
		}
	}
}

// 窗口能缩到最小尺寸。某个控件写死一个过宽的尺寸时，
// 用户会发现窗口怎么也拖不小，而这种事从截图上看不出来。
func TestUIFitsMinimumWindow(t *testing.T) {
	isolateSettings(t)

	got := buildUI().MinSize()

	if got.Width > minWindowWidth {
		t.Errorf("界面最小宽度 %.0f 超过窗口下限 %d，窗口将无法缩到最小尺寸",
			got.Width, minWindowWidth)
	}
	if got.Height > minWindowHeight {
		t.Errorf("界面最小高度 %.0f 超过窗口下限 %d",
			got.Height, minWindowHeight)
	}
}

// 软件已改名，界面上不该再出现苹果的商标
func TestNoAppleTrademarkInAppName(t *testing.T) {
	if strings.Contains(strings.ToLower(appName), "apple") {
		t.Errorf("软件名 %q 仍带 Apple 字样", appName)
	}
}
