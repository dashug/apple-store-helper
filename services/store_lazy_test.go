package services

import (
	"testing"

	"apple-store-helper/model"
)

// GetStore 必须自足：不能依赖调用方先填充过门店表。
//
// 图形版在构建下拉框时顺带调用了 ByArea，命令行没有这一步，
// 于是会得到「未找到门店」—— 而门店其实是存在的。
func TestGetStoreWorksWithoutPriorLoad(t *testing.T) {
	area := model.Areas[0]

	// 清空缓存，模拟从未填充过的状态
	orig := Store.stores
	Store.stores = map[string][]model.Store{}
	t.Cleanup(func() { Store.stores = orig })

	// 先用 ByArea 拿一个真实存在的门店名，再清空后直接查
	name := Store.ByAreaTitleForOptions(area.Title)[0]
	Store.stores = map[string][]model.Store{}

	got, err := Store.GetStore(area.Title, name)
	if err != nil {
		t.Fatalf("门店表未预先填充时应自行加载，实际报错: %v", err)
	}
	if got.CityStoreName != name {
		t.Errorf("取回的门店不对：期望 %q，实际 %q", name, got.CityStoreName)
	}
}

// AddMany 同样不能依赖预先填充 —— 命令行直接走这条路
func TestAddManyWorksWithoutPriorLoad(t *testing.T) {
	area := model.Areas[0]

	origStores := Store.stores
	t.Cleanup(func() { Store.stores = origStores })

	store := Store.ByAreaTitleForOptions(area.Title)[0]
	product := Product.ByAreaTitleForOptions(area.Title)[0]

	Store.stores = map[string][]model.Store{}

	svc := newListenService()
	svc.SetArea(area)

	added, err := svc.AddMany(area.Title, []string{store}, []string{product})
	if err != nil {
		t.Fatalf("未预先填充门店表时添加失败: %v", err)
	}
	if added != 1 {
		t.Errorf("应添加 1 项，实际 %d", added)
	}
}
