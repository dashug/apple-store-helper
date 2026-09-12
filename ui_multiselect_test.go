package main

import (
	"reflect"
	"testing"
)

func newTestSelect(options ...string) *multiSelect {
	m := newMultiSelect("搜索", 100)
	m.SetOptions(options)
	return m
}

// 搜索过滤时，被过滤掉的勾选必须保留 ——
// CheckGroup 的选中态跟着 Options 走，不单独维护就会在搜索时静默丢失
func TestSearchPreservesHiddenSelection(t *testing.T) {
	m := newTestSelect("上海-环球港", "上海-南京东路", "北京-三里屯")

	m.Select("上海-环球港")
	m.Select("北京-三里屯")

	// 搜索「北京」后，上海的勾选被隐藏
	m.search.SetText("北京")
	if got := m.group.Options; len(got) != 1 || got[0] != "北京-三里屯" {
		t.Fatalf("过滤结果不对: %v", got)
	}

	m.search.SetText("")
	want := []string{"上海-环球港", "北京-三里屯"}
	if got := m.Selected(); !reflect.DeepEqual(got, want) {
		t.Errorf("清空搜索后勾选应保留\n期望: %v\n实际: %v", want, got)
	}
}

// 「全选」只应作用于当前可见项，否则搜索就失去了意义
func TestSelectAllOnlyAffectsVisible(t *testing.T) {
	m := newTestSelect("上海-环球港", "上海-南京东路", "北京-三里屯")

	m.search.SetText("上海")
	for _, opt := range m.visibleOptions() {
		m.selected[opt] = true
	}
	m.refresh()
	m.search.SetText("")

	want := []string{"上海-南京东路", "上海-环球港"}
	if got := m.Selected(); !reflect.DeepEqual(got, want) {
		t.Errorf("全选只应选中可见项\n期望: %v\n实际: %v", want, got)
	}
}

// 切换地区后，旧地区的门店不应残留在选中集合里
func TestSetOptionsDropsStaleSelection(t *testing.T) {
	m := newTestSelect("上海-环球港", "北京-三里屯")
	m.Select("上海-环球港")

	m.SetOptions([]string{"Tokyo-Marunouchi", "Osaka-Shinsaibashi"})

	if got := m.Selected(); len(got) != 0 {
		t.Errorf("换了候选项后旧勾选应被丢弃，实际: %v", got)
	}
}

// 勾选可见项后，取消勾选应生效（而不是被独立集合覆盖回来）
func TestUncheckVisibleRemovesSelection(t *testing.T) {
	m := newTestSelect("A", "B", "C")
	m.Select("A")
	m.Select("B")

	// 模拟用户在界面上只留下 A
	m.onChanged([]string{"A"})

	want := []string{"A"}
	if got := m.Selected(); !reflect.DeepEqual(got, want) {
		t.Errorf("取消勾选未生效\n期望: %v\n实际: %v", want, got)
	}
}

// 搜索状态下取消某项，不应影响被隐藏的勾选
func TestUncheckWhileFilteredKeepsHidden(t *testing.T) {
	m := newTestSelect("上海-环球港", "北京-三里屯")
	m.Select("上海-环球港")
	m.Select("北京-三里屯")

	m.search.SetText("北京")
	m.onChanged(nil) // 用户在过滤状态下取消了北京

	m.search.SetText("")
	want := []string{"上海-环球港"}
	if got := m.Selected(); !reflect.DeepEqual(got, want) {
		t.Errorf("只应取消可见项\n期望: %v\n实际: %v", want, got)
	}
}

func TestClearSelection(t *testing.T) {
	m := newTestSelect("A", "B")
	m.Select("A")
	m.ClearSelection()

	if got := m.Selected(); len(got) != 0 {
		t.Errorf("应清空全部勾选，实际: %v", got)
	}
}
