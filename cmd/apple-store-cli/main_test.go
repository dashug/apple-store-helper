package main

import (
	"strings"
	"testing"

	"apple-store-helper/model"
)

func TestStringListCollectsRepeatedFlags(t *testing.T) {
	var list stringList

	for _, v := range []string{" 上海-环球港 ", "上海-南京东路"} {
		if err := list.Set(v); err != nil {
			t.Fatal(err)
		}
	}

	if len(list) != 2 {
		t.Fatalf("应收集 2 项，实际 %d", len(list))
	}
	if list[0] != "上海-环球港" {
		t.Errorf("应去掉首尾空白，实际 %q", list[0])
	}
}

func TestStringListRejectsEmpty(t *testing.T) {
	var list stringList

	if err := list.Set("   "); err == nil {
		t.Error("空值应被拒绝")
	}
	if len(list) != 0 {
		t.Errorf("被拒绝的值不应进入列表，实际 %+v", list)
	}
}

func TestResolveAreaDefaultsToFirst(t *testing.T) {
	got, err := resolveArea("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != model.Areas[0].Title {
		t.Errorf("未指定时应用第一个地区，实际 %q", got.Title)
	}
}

func TestResolveAreaByTitle(t *testing.T) {
	want := model.Areas[1]

	got, err := resolveArea(want.Title)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != want.Title {
		t.Errorf("期望 %q，实际 %q", want.Title, got.Title)
	}
}

// 地区写错时要列出可选项，否则用户在服务器上无从下手
func TestResolveAreaUnknownListsOptions(t *testing.T) {
	_, err := resolveArea("火星")
	if err == nil {
		t.Fatal("未知地区应报错")
	}

	msg := err.Error()
	if !strings.Contains(msg, "火星") {
		t.Errorf("错误信息应包含输入值: %q", msg)
	}
	for _, area := range model.Areas {
		if !strings.Contains(msg, area.Title) {
			t.Errorf("错误信息应列出可选地区 %q: %q", area.Title, msg)
		}
	}
}
