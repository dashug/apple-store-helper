package services

import (
	"regexp"
	"testing"

	"apple-store-helper/model"
)

var storeNumberPattern = regexp.MustCompile(`^R\d+$`)

// 门店数据由 scripts/fetch_stores.py 从 Apple 零售店列表页抓取生成，
// 这里校验每个地区都能解析出可用的门店列表
func TestStoreDataParsesForAllAreas(t *testing.T) {
	for _, area := range model.Areas {
		stores := Store.ByArea(area)

		if len(stores) == 0 {
			t.Errorf("%s: 门店列表为空", area.Title)
			continue
		}

		numbers := map[string]bool{}
		for _, store := range stores {
			if !storeNumberPattern.MatchString(store.StoreNumber) {
				t.Errorf("%s: 门店号格式异常 %q", area.Title, store.StoreNumber)
			}
			if store.CityStoreName == "" {
				t.Errorf("%s: 门店 %s 缺少名称", area.Title, store.StoreNumber)
			}
			if numbers[store.StoreNumber] {
				t.Errorf("%s: 门店号 %s 重复", area.Title, store.StoreNumber)
			}
			numbers[store.StoreNumber] = true
		}

		t.Logf("%-10s %3d 家门店", area.Title, len(stores))
	}
}

// 门店名是下拉框里的唯一标识，重名会导致用户选中的不是他以为的那家
func TestStoreNamesAreUniquePerArea(t *testing.T) {
	for _, area := range model.Areas {
		seen := map[string]string{}

		for _, store := range Store.ByArea(area) {
			if prev, dup := seen[store.CityStoreName]; dup {
				t.Errorf("%s: 门店名 %q 同时对应 %s 与 %s",
					area.Title, store.CityStoreName, prev, store.StoreNumber)
			}
			seen[store.CityStoreName] = store.StoreNumber
		}
	}
}

// 列表里出现的每一家都必须能被 GetStore 取回，否则用户选得到却加不进监听
func TestEveryListedStoreIsResolvable(t *testing.T) {
	for _, area := range model.Areas {
		for _, name := range Store.ByAreaTitleForOptions(area.Title) {
			if _, err := Store.GetStore(area.Title, name); err != nil {
				t.Errorf("%s: 列表中的 %q 无法取回: %v", area.Title, name, err)
			}
		}
	}
}
