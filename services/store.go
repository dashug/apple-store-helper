package services

import (
	"apple-store-helper/config"
	"apple-store-helper/model"
	"fmt"
	"sort"

	"github.com/thoas/go-funk"
	"github.com/tidwall/gjson"
)

var Store = storeService{
	stores: map[string][]model.Store{},
}

type storeService struct {
	stores map[string][]model.Store
}

func (s *storeService) ByArea(area model.Area) []model.Store {
	stores, err := config.ReadConfigFile("stores.json")
	if err != nil {
		panic(err)
	}

	for _, v := range gjson.ParseBytes(stores).Array() {
		locale := v.Get("locale").String()
		hasStates := v.Get("hasStates").Bool()

		localeStores := []model.Store{}

		if hasStates {
			for _, state := range v.Get("state").Array() {
				for _, store := range state.Get("store").Array() {
					localeStores = append(localeStores, model.Store{
						StoreNumber:   store.Get("id").String(),
						CityStoreName: fmt.Sprintf("%s-%s", store.Get("address.stateName").String(), store.Get("name").String()),
					})
				}
			}
		} else {
			for _, store := range v.Get("store").Array() {
				localeStores = append(localeStores, model.Store{
					StoreNumber:   store.Get("id").String(),
					CityStoreName: fmt.Sprintf("%s-%s", store.Get("address.city").String(), store.Get("name").String()),
				})
			}
		}

		// 去重
		localeStores = funk.UniqBy(localeStores, func(x model.Store) string {
			return x.StoreNumber
		}).([]model.Store)

		s.stores[locale] = localeStores
	}

	return s.stores[area.Locale]
}

func (s *storeService) ByAreaTitleForOptions(areaTitle string) []string {
	area := Area.GetArea(areaTitle)
	areas := funk.Get(s.ByArea(area), "CityStoreName").([]string)
	sort.Strings(areas)
	return areas
}

func (s *storeService) GetStore(areaTitle string, storeTitle string) (model.Store, error) {
	code := Area.Title2Code(areaTitle)
	if code == "" {
		return model.Store{}, fmt.Errorf("未知地区: %s", areaTitle)
	}

	// 门店表是懒加载的。图形版在构建下拉框时顺带填充了它，命令行没有这一步，
	// 不在这里兜底就会得到「未找到门店」——而门店其实是存在的。
	if len(s.stores[code]) == 0 {
		s.ByArea(Area.GetArea(areaTitle))
	}

	// funk.Find 找不到时返回 nil，必须先判空再断言
	found := funk.Find(s.stores[code], func(x model.Store) bool {
		return x.CityStoreName == storeTitle
	})
	if found == nil {
		return model.Store{}, fmt.Errorf("未找到门店「%s」，门店列表可能已更新，请重新选择", storeTitle)
	}

	return found.(model.Store), nil
}
