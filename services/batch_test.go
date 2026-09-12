package services

import (
	"testing"

	"apple-store-helper/model"
)

func realArea() string { return model.Areas[0].Title }

func realStoresAndProducts(t *testing.T, n int) ([]string, []string) {
	t.Helper()

	stores := Store.ByAreaTitleForOptions(realArea())
	products := Product.ByAreaTitleForOptions(realArea())
	if len(stores) < n || len(products) < n {
		t.Fatalf("内置数据不足: 门店 %d, 型号 %d", len(stores), len(products))
	}
	return stores[:n], products[:n]
}

// 批量添加是这次改造的核心：一次勾选应产生「门店 × 型号」的全部组合
func TestAddManyCreatesCartesianProduct(t *testing.T) {
	stores, products := realStoresAndProducts(t, 3)
	svc := newListenService()

	added, err := svc.AddMany(realArea(), stores, products)
	if err != nil {
		t.Fatalf("批量添加失败: %v", err)
	}

	want := len(stores) * len(products)
	if added != want {
		t.Errorf("应新增 %d 项，实际 %d", want, added)
	}
	if got := len(svc.GetListenItems()); got != want {
		t.Errorf("监听列表应有 %d 项，实际 %d", want, got)
	}
}

// 重复添加不应产生重复项，也不应重置已有状态
func TestAddManySkipsExisting(t *testing.T) {
	stores, products := realStoresAndProducts(t, 2)
	svc := newListenService()

	if _, err := svc.AddMany(realArea(), stores, products); err != nil {
		t.Fatal(err)
	}

	var key string
	for k := range svc.GetListenItems() {
		key = k
		break
	}
	svc.UpdateStatus(key, StatusInStock, "")

	added, err := svc.AddMany(realArea(), stores, products)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("重复添加应新增 0 项，实际 %d", added)
	}
	if got := svc.GetListenItems()[key].Status; got != StatusInStock {
		t.Errorf("已有项的状态不应被重置，实际 %q", got)
	}
}

// 任一门店/型号解析失败时整体失败，避免用户以为全加上了
func TestAddManyFailsAtomically(t *testing.T) {
	stores, products := realStoresAndProducts(t, 2)
	svc := newListenService()

	_, err := svc.AddMany(realArea(), append(stores, "不存在的门店"), products)
	if err == nil {
		t.Fatal("含无效门店时应返回错误")
	}
	if got := len(svc.GetListenItems()); got != 0 {
		t.Errorf("失败时不应写入任何项，实际 %d 项", got)
	}
}

// 此前只能整体「清空」，加错一条就得全部重来
func TestRemoveDeletesSingleItem(t *testing.T) {
	stores, products := realStoresAndProducts(t, 2)
	svc := newListenService()

	if _, err := svc.AddMany(realArea(), stores, products); err != nil {
		t.Fatal(err)
	}

	rows := svc.SortedRows()
	before := len(rows)
	svc.Remove(rows[0].Key)

	after := svc.GetListenItems()
	if len(after) != before-1 {
		t.Errorf("应只删除 1 项，%d -> %d", before, len(after))
	}
	if _, still := after[rows[0].Key]; still {
		t.Error("目标项未被删除")
	}
}

// 有货必须排在最前，否则一屏几十条里根本看不到命中的那条
func TestSortedRowsPutsInStockFirst(t *testing.T) {
	stores, products := realStoresAndProducts(t, 3)
	svc := newListenService()

	if _, err := svc.AddMany(realArea(), stores, products); err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0)
	for _, r := range svc.SortedRows() {
		keys = append(keys, r.Key)
	}

	svc.UpdateStatus(keys[len(keys)-1], StatusInStock, "")
	svc.UpdateStatus(keys[len(keys)-2], StatusUnknown, "超时")
	for _, k := range keys[:len(keys)-2] {
		svc.UpdateStatus(k, StatusOutStock, "")
	}

	rows := svc.SortedRows()
	if rows[0].Status != StatusInStock {
		t.Errorf("有货应排第一，实际 %q", rows[0].Status)
	}
	if rows[1].Status != StatusUnknown {
		t.Errorf("未知应排第二，实际 %q", rows[1].Status)
	}
}

// 顺序必须稳定，否则列表每轮刷新都乱跳，用户点不中删除按钮
func TestSortedRowsIsStable(t *testing.T) {
	stores, products := realStoresAndProducts(t, 3)
	svc := newListenService()

	if _, err := svc.AddMany(realArea(), stores, products); err != nil {
		t.Fatal(err)
	}

	first := svc.SortedRows()
	for i := 0; i < 5; i++ {
		again := svc.SortedRows()
		for j := range first {
			if first[j].Key != again[j].Key {
				t.Fatalf("第 %d 次调用顺序发生变化，位置 %d: %s != %s", i, j, first[j].Key, again[j].Key)
			}
		}
	}
}

// 列表变化必须通知界面刷新
func TestOnChangeFires(t *testing.T) {
	stores, products := realStoresAndProducts(t, 1)
	svc := newListenService()

	calls := 0
	svc.SetOnChange(func() { calls++ })

	if _, err := svc.AddMany(realArea(), stores, products); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Error("添加后应触发刷新")
	}

	before := calls
	svc.Remove(svc.SortedRows()[0].Key)
	if calls == before {
		t.Error("删除后应触发刷新")
	}

	before = calls
	svc.Clean()
	if calls == before {
		t.Error("清空后应触发刷新")
	}
}
