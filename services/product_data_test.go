package services

import (
	"strings"
	"testing"

	"apple-store-helper/model"
)

// 机型数据由 scripts/fetch_products.py 从 Apple 购买页抓取生成，
// 这里校验每个地区的数据都能被正确解析成可用的型号列表
func TestProductDataParsesForAllAreas(t *testing.T) {
	for _, area := range model.Areas {
		products := Area.ProductsByCode(area.Locale)

		if len(products) == 0 {
			t.Errorf("%s: 解析结果为空", area.Locale)
			continue
		}

		codes := map[string]bool{}
		families := map[string]bool{}

		for _, p := range products {
			// 标题形如 "familyType - 颜色 - 容量"，任一段缺失都会让用户无法辨认
			parts := strings.Split(p.Title, " - ")
			if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
				t.Errorf("%s: 标题格式异常 %q", area.Locale, p.Title)
			}
			if p.Code == "" {
				t.Errorf("%s: %q 缺少货号", area.Locale, p.Title)
			}
			if codes[p.Code] {
				t.Errorf("%s: 货号 %s 重复", area.Locale, p.Code)
			}
			codes[p.Code] = true
			families[p.Type] = true
		}

		t.Logf("%s: %d 个型号, %d 个系列", area.Locale, len(products), len(families))
	}
}

// 下拉框里出现多个同名项会让用户无法选择，日本站的原始数据就有这个问题
func TestProductTitlesAreUnique(t *testing.T) {
	for _, area := range model.Areas {
		seen := map[string]bool{}
		for _, p := range Area.ProductsByCode(area.Locale) {
			if seen[p.Title] {
				t.Errorf("%s: 型号名称重复 %q", area.Locale, p.Title)
			}
			seen[p.Title] = true
		}
	}
}

// 未知地区不应 panic
func TestProductsByCodeUnknownLocale(t *testing.T) {
	if got := Area.ProductsByCode("xx_XX"); len(got) != 0 {
		t.Errorf("未知地区应返回空列表，实际 %d 项", len(got))
	}
}
