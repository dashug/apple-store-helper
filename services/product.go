package services

import (
	"fmt"

	"apple-store-helper/model"

	"github.com/thoas/go-funk"
)

var Product = productService{}

type productService struct{}

func (s *productService) ByAreaTitleForOptions(areaTitle string) []string {
	code := Area.Title2Code(areaTitle)
	return funk.Get(Area.ProductsByCode(code), "Title").([]string)
}

func (s *productService) GetProduct(areaTitle string, productTitle string) (model.Product, error) {
	code := Area.Title2Code(areaTitle)
	if code == "" {
		return model.Product{}, fmt.Errorf("未知地区: %s", areaTitle)
	}

	// funk.Find 找不到时返回 nil，必须先判空再断言
	found := funk.Find(Area.ProductsByCode(code), func(x model.Product) bool {
		return x.Title == productTitle
	})
	if found == nil {
		return model.Product{}, fmt.Errorf("未找到型号「%s」，机型列表可能已更新，请重新选择", productTitle)
	}

	return found.(model.Product), nil
}
