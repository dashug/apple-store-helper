package services

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"apple-store-helper/model"
)

// TestLiveEndpointHealth 用真实门店与货号访问 Apple 的库存接口，确认它仍然可用。
//
// 这个项目此前整整一段时间是失效的：所用接口对任意请求恒返 HTTP 541 拦截页，
// 而旧代码不检查状态码，把拦截页解析成「所有型号无货」——界面一片「无货」，
// 没有任何异常提示，也没有任何人知道。
//
// 现在用的接口同样会有失效的一天。这个用例由每日工作流执行，失效时自动开
// issue，把「等用户发现」变成「维护者提前知道」。
//
//	LIVE_CHECK=1 go test -run TestLiveEndpointHealth ./services/
func TestLiveEndpointHealth(t *testing.T) {
	if os.Getenv("LIVE_CHECK") == "" {
		t.Skip("设置 LIVE_CHECK=1 执行联网检查")
	}

	var failures []string

	for _, area := range model.Areas {
		stores := Store.ByArea(area)
		products := Area.ProductsByCode(area.Locale)

		if len(stores) == 0 || len(products) == 0 {
			failures = append(failures, fmt.Sprintf("%s: 缺少门店或型号数据", area.Title))
			continue
		}

		svc := newListenService()
		svc.SetArea(area)

		item := ListenItem{Store: stores[0], Product: products[0], Area: area.Title}
		key := item.Store.StoreNumber + "." + item.Product.Code

		skus, errs, _ := svc.groupByStore(map[string]ListenItem{key: item})

		if reason, bad := errs[item.Store.StoreNumber]; bad {
			failures = append(failures,
				fmt.Sprintf("%s: 查询 %s 失败 —— %s", area.Title, item.Store.CityStoreName, reason))
			continue
		}

		// 能解析出这个 SKU，才说明响应结构仍是我们预期的样子
		if _, ok := skus[key]; !ok {
			failures = append(failures,
				fmt.Sprintf("%s: 响应中没有 %s 的库存数据，接口结构可能已变更",
					area.Title, item.Product.Code))
			continue
		}

		t.Logf("%-10s %-26s %s  有货=%v", area.Title, item.Store.CityStoreName, item.Product.Code, skus[key])
	}

	if len(failures) > 0 {
		t.Fatalf("接口健康检查未通过：\n  %s", strings.Join(failures, "\n  "))
	}
}
