package services

import (
	"testing"

	"apple-store-helper/model"
)

// 找两个都有门店与型号数据的地区
func twoAreas(t *testing.T) (model.Area, model.Area) {
	t.Helper()

	var usable []model.Area
	for _, a := range model.Areas {
		if len(Store.ByArea(a)) > 0 && len(Area.ProductsByCode(a.Locale)) > 0 {
			usable = append(usable, a)
		}
		if len(usable) == 2 {
			return usable[0], usable[1]
		}
	}

	t.Fatal("可用地区不足 2 个")
	return model.Area{}, model.Area{}
}

func addOne(t *testing.T, svc *listenService, area model.Area) {
	t.Helper()

	stores := Store.ByAreaTitleForOptions(area.Title)
	products := Product.ByAreaTitleForOptions(area.Title)

	svc.SetArea(area)
	if _, err := svc.AddMany(area.Title, stores[:1], products[:1]); err != nil {
		t.Fatalf("%s 添加失败: %v", area.Title, err)
	}
}

// 这是本次修复的核心：切换地区不得清空已配置的监听列表。
// 地区选择器就在界面顶部，误点一下就丢掉全部配置且不可恢复。
func TestSwitchingAreaKeepsOtherAreaList(t *testing.T) {
	first, second := twoAreas(t)
	svc := newListenService()

	addOne(t, svc, first)
	addOne(t, svc, second)

	// 两个地区的项都应保留在总表里
	if got := len(svc.GetListenItems()); got != 2 {
		t.Fatalf("两个地区共应有 2 项，实际 %d", got)
	}

	// 切回第一个地区，它的项必须还在
	svc.SetArea(first)
	if got := len(svc.CurrentAreaItems()); got != 1 {
		t.Errorf("切回 %s 后应有 1 项，实际 %d", first.Title, got)
	}

	svc.SetArea(second)
	if got := len(svc.CurrentAreaItems()); got != 1 {
		t.Errorf("切到 %s 后应有 1 项，实际 %d", second.Title, got)
	}
}

// 展示与监听都只应看到当前地区：其他地区的货号在本地区接口上查不到
func TestOnlyCurrentAreaIsVisible(t *testing.T) {
	first, second := twoAreas(t)
	svc := newListenService()

	addOne(t, svc, first)
	addOne(t, svc, second)

	svc.SetArea(first)
	rows := svc.SortedRows()
	if len(rows) != 1 {
		t.Fatalf("应只显示当前地区的 1 项，实际 %d", len(rows))
	}
	if rows[0].Area != first.Title {
		t.Errorf("显示了其他地区的项: %q", rows[0].Area)
	}
}

// 「清空」清的是看得见的那部分，不应连带清掉其他地区
func TestCleanOnlyClearsCurrentArea(t *testing.T) {
	first, second := twoAreas(t)
	svc := newListenService()

	addOne(t, svc, first)
	addOne(t, svc, second)

	svc.SetArea(first)
	svc.Clean()

	if got := len(svc.CurrentAreaItems()); got != 0 {
		t.Errorf("当前地区应已清空，实际 %d 项", got)
	}

	svc.SetArea(second)
	if got := len(svc.CurrentAreaItems()); got != 1 {
		t.Errorf("其他地区不应被清空，实际 %d 项", got)
	}
}

func TestCleanAllClearsEverything(t *testing.T) {
	first, second := twoAreas(t)
	svc := newListenService()

	addOne(t, svc, first)
	addOne(t, svc, second)

	svc.CleanAll()

	if got := len(svc.GetListenItems()); got != 0 {
		t.Errorf("应全部清空，实际 %d 项", got)
	}
}

// 旧配置文件没有 Area 字段，升级后必须归入当前地区，否则列表整个消失
func TestLegacyItemsWithoutAreaAreAdopted(t *testing.T) {
	area := model.Areas[0]
	svc := newListenService()
	svc.SetArea(area)

	svc.SetListenItems(map[string]ListenItem{
		"R683.MJYH4CH/A": {
			Store:   model.Store{StoreNumber: "R683", CityStoreName: "上海-环球港"},
			Product: model.Product{Code: "MJYH4CH/A", Title: "iPhone 18 Pro Max"},
			Status:  StatusWait,
			// 没有 Area
		},
	})

	if got := len(svc.CurrentAreaItems()); got != 1 {
		t.Fatalf("旧配置项应归入当前地区，实际当前地区 %d 项", got)
	}
	if got := svc.GetListenItems()["R683.MJYH4CH/A"].Area; got != area.Title {
		t.Errorf("Area 应补为 %q，实际 %q", area.Title, got)
	}
}

// 切换地区要通知界面刷新，否则列表还停留在上一个地区
func TestSetAreaTriggersRefresh(t *testing.T) {
	svc := newListenService()

	calls := 0
	svc.SetOnChange(func() { calls++ })

	svc.SetArea(model.Areas[1])
	if calls == 0 {
		t.Error("切换地区后应触发界面刷新")
	}
}
