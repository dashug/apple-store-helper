package main

import (
	"strings"
	"testing"

	"apple-store-helper/services"
)

// 摘要要按命中次数排序，用户据此判断该重点盯哪几家
func TestHistorySummaryRanksByHitCount(t *testing.T) {
	entries := []services.HistoryEntry{
		{Store: "北京-三里屯"},
		{Store: "上海-环球港"}, {Store: "上海-环球港"}, {Store: "上海-环球港"},
		{Store: "深圳-益田假日广场"}, {Store: "深圳-益田假日广场"},
	}

	got := historySummary(entries)

	if !strings.Contains(got, "共 6 次命中") || !strings.Contains(got, "3 家门店") {
		t.Errorf("摘要缺少总数: %q", got)
	}

	hot := strings.Index(got, "上海-环球港")
	mid := strings.Index(got, "深圳-益田假日广场")
	cold := strings.Index(got, "北京-三里屯")

	if !(hot < mid && mid < cold) {
		t.Errorf("未按命中次数排序: %q", got)
	}
}

// 门店多时只列前几家，再多对判断没有帮助
func TestHistorySummaryTruncates(t *testing.T) {
	var entries []services.HistoryEntry
	for _, s := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		entries = append(entries, services.HistoryEntry{Store: s})
	}

	got := historySummary(entries)
	if !strings.Contains(got, "其余 2 家从略") {
		t.Errorf("应折叠多余门店: %q", got)
	}
}

func TestHistorySummaryWithSingleStore(t *testing.T) {
	got := historySummary([]services.HistoryEntry{{Store: "上海-环球港"}})

	if !strings.Contains(got, "共 1 次命中") || !strings.Contains(got, "1 家门店") {
		t.Errorf("摘要不对: %q", got)
	}
	if strings.Contains(got, "从略") {
		t.Errorf("只有一家时不应出现折叠提示: %q", got)
	}
}
