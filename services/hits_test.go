package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"apple-store-helper/model"
)

// 让两个门店都返回有货
const twoHitBody = `{"body":{"stores":[
{"storeNumber":"R001","partsAvailability":{"P1":{"messageTypes":{"compact":{"storeSelectionEnabled":true}}}}},
{"storeNumber":"R002","partsAvailability":{"P1":{"messageTypes":{"compact":{"storeSelectionEnabled":true}}}}}
]}}`

func serveHits(t *testing.T, body string) func() {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	orig := pickupBaseURL
	pickupBaseURL = srv.URL

	return func() {
		pickupBaseURL = orig
		srv.Close()
	}
}

// 两个门店 + 一个型号，门店名刻意逆序以便验证排序
func twoStoreService(t *testing.T) (*listenService, []string) {
	t.Helper()

	svc := newListenService()
	svc.SetArea(model.Areas[0])

	items := map[string]ListenItem{}
	keys := []string{}
	for _, spec := range []struct{ id, name string }{{"R002", "乙-第二家"}, {"R001", "甲-第一家"}} {
		key := spec.id + ".P1"
		items[key] = ListenItem{
			Store:   model.Store{StoreNumber: spec.id, CityStoreName: spec.name},
			Product: model.Product{Code: "P1", Title: "iphone18pro"},
			Area:    model.Areas[0].Title,
		}
		keys = append(keys, key)
	}
	svc.SetListenItems(items)
	svc.SetStatus(Running)

	return svc, keys
}

// 同一轮里所有命中都要被标记，而不是只标第一个
func TestAllHitsAreMarked(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	svc, keys := twoStoreService(t)
	svc.tick()

	for _, key := range keys {
		if got := svc.GetListenItems()[key].Status; got != StatusInStock {
			t.Errorf("%s 应标记为有货，实际 %q", key, got)
		}
	}
}

// 事件要带上全部命中：多家同时有货时，用户据此决定去哪一家
func TestEventCarriesAllHits(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	svc, _ := twoStoreService(t)

	var (
		mu    sync.Mutex
		event InStockEvent
	)
	svc.SetOnInStock(func(e InStockEvent) {
		mu.Lock()
		event = e
		mu.Unlock()
	})

	svc.tick()

	mu.Lock()
	defer mu.Unlock()

	if len(event.Items) != 2 {
		t.Fatalf("应报告 2 项命中，实际 %d", len(event.Items))
	}
	for _, name := range []string{"甲-第一家", "乙-第二家"} {
		if !strings.Contains(event.Message, name) {
			t.Errorf("汇总文案应包含 %q: %q", name, event.Message)
		}
	}
}

// 顺序必须确定：map 遍历无序，不排序则每次「第一家」都可能不同
func TestHitOrderIsDeterministic(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	var seen []string

	for i := 0; i < 8; i++ {
		svc, _ := twoStoreService(t)

		var (
			mu    sync.Mutex
			first string
		)
		svc.SetOnInStock(func(e InStockEvent) {
			mu.Lock()
			if len(e.Items) > 0 {
				first = e.Items[0].Store.CityStoreName
			}
			mu.Unlock()
		})

		svc.tick()

		mu.Lock()
		seen = append(seen, first)
		mu.Unlock()
	}

	// 断言的是「每次都一样」，而不是某个具体的名字 ——
	// 排序按码点而非汉语习惯（乙 U+4E59 在 甲 U+7532 之前），
	// 把某个名字写死进断言，测的就成了我对排序的想当然。
	for i, name := range seen {
		if name != seen[0] {
			t.Fatalf("首个命中在第 %d 次发生变化：%q != %q（全部: %v）", i, name, seen[0], seen)
		}
	}

	// 且应当是门店名中字典序最小的那个
	want := "甲-第一家"
	if "乙-第二家" < want {
		want = "乙-第二家"
	}
	if seen[0] != want {
		t.Errorf("首个命中应为字典序最小的门店 %q，实际 %q", want, seen[0])
	}
}

// 命中后默认暂停
func TestStopsOnHitByDefault(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	svc, _ := twoStoreService(t)
	svc.tick()

	if got := svc.GetStatus(); got != Pause {
		t.Errorf("默认应命中即暂停，实际 %q", got)
	}
}

// 关掉自动暂停后继续监听
func TestKeepsGoingWhenConfigured(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	svc, _ := twoStoreService(t)
	svc.SetStopOnHit(false)
	svc.tick()

	if got := svc.GetStatus(); got != Running {
		t.Errorf("关闭自动暂停后应继续监听，实际 %q", got)
	}
}

// 命中项之外的其余项也要在同一轮被更新，不能停留在旧状态
func TestNonHitItemsAreStillUpdated(t *testing.T) {
	withTempConfigDir(t)

	// R001 有货，R002 无货
	body := `{"body":{"stores":[
	{"storeNumber":"R001","partsAvailability":{"P1":{"messageTypes":{"compact":{"storeSelectionEnabled":true}}}}},
	{"storeNumber":"R002","partsAvailability":{"P1":{"messageTypes":{"compact":{"storeSelectionEnabled":false}}}}}
	]}}`
	defer serveHits(t, body)()

	svc, _ := twoStoreService(t)

	// 先把两项都置为「等待」，若未被更新即可看出
	for key := range svc.GetListenItems() {
		svc.UpdateStatus(key, StatusWait, "")
	}

	svc.tick()

	items := svc.GetListenItems()
	if got := items["R001.P1"].Status; got != StatusInStock {
		t.Errorf("有货项应为 %q，实际 %q", StatusInStock, got)
	}
	if got := items["R002.P1"].Status; got != StatusOutStock {
		t.Errorf("无货项应在同一轮被更新为 %q，实际 %q", StatusOutStock, got)
	}
}

// 全部命中都要记进有货历史
func TestAllHitsAreRecorded(t *testing.T) {
	withTempConfigDir(t)
	defer serveHits(t, twoHitBody)()

	svc, _ := twoStoreService(t)
	svc.tick()

	entries, err := LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("两项命中应产生 2 条记录，实际 %d", len(entries))
	}
}

func TestHitMessageSingleVsMultiple(t *testing.T) {
	one := []ListenItem{{Store: model.Store{CityStoreName: "上海-环球港"}, Product: model.Product{Title: "A"}}}
	if got := hitMessage(one); got != "上海-环球港 A 有货" {
		t.Errorf("单项文案不对: %q", got)
	}

	two := append(one, ListenItem{Store: model.Store{CityStoreName: "北京-三里屯"}, Product: model.Product{Title: "B"}})
	got := hitMessage(two)
	if !strings.Contains(got, "2 项有货") {
		t.Errorf("多项文案应标出数量: %q", got)
	}
	for _, want := range []string{"上海-环球港", "北京-三里屯"} {
		if !strings.Contains(got, want) {
			t.Errorf("多项文案应列出 %q: %q", want, got)
		}
	}
}

var _ = fmt.Sprintf
