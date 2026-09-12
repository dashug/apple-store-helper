package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 新版 /shop/retail/pickup-message 的结构
const newShapeBody = `{"body":{"stores":[{"storeNumber":"R683","storeName":"环球港",
"partsAvailability":{"MJYH4CH/A":{"partNumber":"MJYH4CH/A","messageTypes":{"compact":
{"storeSelectionEnabled":true,"storePickupQuote":"今天"}}},
"MJTC4CH/A":{"partNumber":"MJTC4CH/A","messageTypes":{"compact":
{"storeSelectionEnabled":false,"storePickupQuote":"暂无供应"}}}}}]}}`

// 旧版 /shop/fulfillment-messages 的嵌套结构
const oldShapeBody = `{"body":{"content":{"pickupMessage":{"stores":[{"storeNumber":"R409",
"partsAvailability":{"MJYH4ZA/A":{"messageTypes":{"compact":{"storeSelectionEnabled":true}}}}}]}}}}`

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchStoreParsesNewShape(t *testing.T) {
	res := fetchStore("R683", serve(t, 200, newShapeBody))

	if res.err != nil {
		t.Fatalf("不应报错: %v", res.err)
	}
	if got := res.skus["R683.MJYH4CH/A"]; !got {
		t.Error("有货的型号应为 true")
	}
	if got, ok := res.skus["R683.MJTC4CH/A"]; !ok || got {
		t.Error("无货的型号应为 false 且存在")
	}
}

// 保留对旧结构的兼容，以防 Apple 把数据挪回去
func TestFetchStoreFallsBackToOldShape(t *testing.T) {
	res := fetchStore("R409", serve(t, 200, oldShapeBody))

	if res.err != nil {
		t.Fatalf("不应报错: %v", res.err)
	}
	if !res.skus["R409.MJYH4ZA/A"] {
		t.Error("旧结构未被正确解析")
	}
}

// 这是线上真实的故障形态：接口返回 541 加一个 HTML 拦截页。
// 必须报错，否则 HTML 解析不出东西会被当成「所有型号无货」。
func TestFetchStoreRejectsBlockPage(t *testing.T) {
	html := "<!doctype html><html><head><title>Page Not Found - Apple</title></head><body>…</body></html>"
	res := fetchStore("R683", serve(t, 541, html))

	if res.err == nil {
		t.Fatal("拦截页必须报错，不能被当成无货")
	}
	if !strings.Contains(res.err.Error(), "541") {
		t.Errorf("错误信息应包含状态码，实际: %v", res.err)
	}
	if len(res.skus) != 0 {
		t.Error("报错时不应返回任何库存判断")
	}
}

func TestFetchStoreReportsAppleErrorMessage(t *testing.T) {
	body := `{"body":{"errorMessage":"请输入有效的省/市名称或邮政编码。"}}`
	res := fetchStore("R683", serve(t, 200, body))

	if res.err == nil {
		t.Fatal("接口报错时应返回错误")
	}
	if !strings.Contains(res.err.Error(), "有效的省") {
		t.Errorf("应带上接口给的原因，实际: %v", res.err)
	}
}

// 接口结构变化时必须让用户看见，而不是静默变成无货
func TestFetchStoreRejectsUnknownShape(t *testing.T) {
	res := fetchStore("R683", serve(t, 200, `{"body":{"somethingElse":[]}}`))

	if res.err == nil {
		t.Fatal("无法识别的结构应报错")
	}
	if !strings.Contains(res.err.Error(), "接口可能已变更") {
		t.Errorf("错误信息应提示接口变更，实际: %v", res.err)
	}
}

func TestFetchStoreRejectsEmptyStores(t *testing.T) {
	res := fetchStore("R683", serve(t, 200, `{"body":{"stores":[]}}`))

	if res.err == nil {
		t.Fatal("没有门店数据时应报错，而不是判定为无货")
	}
}

func TestFetchStoreReportsNetworkError(t *testing.T) {
	res := fetchStore("R683", "http://127.0.0.1:1/nope")

	if res.err == nil {
		t.Fatal("网络错误应返回错误")
	}
	if !strings.Contains(res.err.Error(), "网络错误") {
		t.Errorf("错误信息应标明网络错误，实际: %v", res.err)
	}
}

// 全部查不到结果时，日志顶部必须有显著告警，
// 否则用户会把一屏「未知」当成「都没货」
func TestLogShowsWarningWhenAllUnknown(t *testing.T) {
	svc := newListenService()
	svc.SetListenItems(map[string]ListenItem{"R683.MJYH4CH/A": testItem()})
	svc.UpdateStatus("R683.MJYH4CH/A", StatusUnknown, "接口返回 HTTP 541")

	svc.mu.RLock()
	text := svc.logText()
	svc.mu.RUnlock()

	if !strings.Contains(text, "不可信") {
		t.Errorf("全部未知时应有整体告警，实际:\n%s", text)
	}
	if !strings.Contains(text, "HTTP 541") {
		t.Errorf("应显示未知的具体原因，实际:\n%s", text)
	}
}

func TestLogHasNoWarningWhenSomeKnown(t *testing.T) {
	svc := newListenService()
	svc.SetListenItems(map[string]ListenItem{
		"R683.MJYH4CH/A": testItem(),
		"R409.MJTC4CH/A": testItem(),
	})
	svc.UpdateStatus("R683.MJYH4CH/A", StatusUnknown, "超时")
	svc.UpdateStatus("R409.MJTC4CH/A", StatusOutStock, "")

	svc.mu.RLock()
	text := svc.logText()
	svc.mu.RUnlock()

	if strings.Contains(text, "不可信") {
		t.Error("仍有可信结果时不应显示整体告警")
	}
}
