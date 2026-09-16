package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"apple-store-helper/model"
)

// stubStores 起一个按门店编号决定成败的接口。
// fail 中的门店返回 500，其余返回有货。
func stubStores(t *testing.T, fail map[string]bool) (*httptest.Server, func(string) int) {
	t.Helper()

	var mu sync.Mutex
	hits := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := r.URL.Query().Get("store")

		mu.Lock()
		hits[store]++
		failing := fail[store]
		mu.Unlock()

		if failing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, _ = w.Write([]byte(`{"body":{"stores":[{"storeNumber":"` + store +
			`","partsAvailability":{"P1":{"messageTypes":{"compact":{"storeSelectionEnabled":false}}}}}]}}`))
	}))

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	t.Cleanup(func() {
		pickupBaseURL = orig
		srv.Close()
	})

	return srv, func(store string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[store]
	}
}

// shortenStoreSkip 把门店退避的基数缩到毫秒级，便于测试观察
func shortenStoreSkip(t *testing.T, d time.Duration) {
	t.Helper()

	orig := storeSkipBase
	storeSkipBase = d
	t.Cleanup(func() { storeSkipBase = orig })
}

func twoStores(t *testing.T) *listenService {
	t.Helper()

	svc := newListenService()
	svc.SetArea(model.Areas[0])
	svc.SetInterval(5 * time.Second)
	svc.SetStopOnHit(false)

	items := map[string]ListenItem{}
	for _, sn := range []string{"R001", "R002"} {
		items[sn+".P1"] = ListenItem{
			Store:   model.Store{StoreNumber: sn, CityStoreName: sn + "-门店"},
			Product: model.Product{Code: "P1", Title: "iphone18pro"},
			Area:    model.Areas[0].Title,
		}
	}
	svc.SetListenItems(items)
	svc.SetStatus(Running)

	return svc
}

// 一家门店坏掉，不该把其余健康门店的查询频率一起拖垮
func TestOneBadStoreDoesNotSlowHealthyStores(t *testing.T) {
	stubStores(t, map[string]bool{"R001": true})
	shortenStoreSkip(t, time.Millisecond)

	svc := twoStores(t)
	base := svc.GetInterval()

	for round := 1; round <= 8; round++ {
		delay := svc.nextDelay(svc.tick())

		// 抖动是 ±25%，超出这个范围就说明退避被触发了
		if delay > base*3/2 {
			t.Fatalf("第 %d 轮后等待 %v，已超出基础间隔 %v 的抖动范围 —— "+
				"一家门店失败不应拖慢其余门店", round, delay, base)
		}
	}
}

// 所有门店都失败才是全局问题（限流或接口下线），这时才该退避
func TestAllStoresFailingBacksOff(t *testing.T) {
	stubStores(t, map[string]bool{"R001": true, "R002": true})
	shortenStoreSkip(t, time.Hour) // 不让门店级退避干扰观察

	svc := twoStores(t)
	base := svc.GetInterval()

	var delay time.Duration
	for round := 1; round <= 3; round++ {
		delay = svc.nextDelay(svc.tick())
	}

	if delay <= base*3/2 {
		t.Fatalf("全部门店失败三轮后等待仍是 %v，应已退避到远大于基础间隔 %v", delay, base)
	}
}

// 连续失败的门店应被暂时跳过，不再每轮都去撞
func TestBadStoreIsSkippedAfterRepeatedFailures(t *testing.T) {
	_, hits := stubStores(t, map[string]bool{"R001": true})
	shortenStoreSkip(t, time.Hour) // 一旦进入退避，本用例期间不恢复

	svc := twoStores(t)

	for round := 1; round <= storeFailureThreshold+3; round++ {
		svc.tick()
	}

	if got := hits("R001"); got > storeFailureThreshold {
		t.Errorf("坏掉的门店被查询 %d 次，连续失败 %d 次后就应暂停查询",
			got, storeFailureThreshold)
	}
	if got := hits("R002"); got != storeFailureThreshold+3 {
		t.Errorf("健康门店应每轮都查，期望 %d 次，实际 %d 次",
			storeFailureThreshold+3, got)
	}
}

// 被跳过的门店，界面上要说明为什么没有结果
func TestSkippedStoreExplainsItself(t *testing.T) {
	stubStores(t, map[string]bool{"R001": true})
	shortenStoreSkip(t, time.Hour)

	svc := twoStores(t)
	for round := 1; round <= storeFailureThreshold+1; round++ {
		svc.tick()
	}

	item := svc.GetListenItems()["R001.P1"]
	if item.Status != StatusUnknown {
		t.Fatalf("被跳过的门店状态应为 %q，实际 %q", StatusUnknown, item.Status)
	}

	// 只断言「非空」是不够的：查询失败时原因串同样非空，
	// 那样即使根本没跳过，用例也照样通过
	if !strings.Contains(item.Detail, "暂停查询") {
		t.Fatalf("应说明该门店已被暂停查询，实际原因是 %q", item.Detail)
	}
}

// 跳过不是「查询失败」，不该喂给全局退避
func TestSkippedStoreDoesNotFeedGlobalBackoff(t *testing.T) {
	stubStores(t, map[string]bool{"R001": true})
	shortenStoreSkip(t, time.Hour)

	svc := twoStores(t)
	base := svc.GetInterval()

	for round := 1; round <= storeFailureThreshold+5; round++ {
		delay := svc.nextDelay(svc.tick())
		if delay > base*3/2 {
			t.Fatalf("第 %d 轮后等待 %v 超出基础间隔 %v 的抖动范围", round, delay, base)
		}
	}
}

// 门店恢复后应立刻回到正常查询，不需要用户干预
func TestStoreRecoversOnItsOwn(t *testing.T) {
	fail := map[string]bool{"R001": true}
	_, hits := stubStores(t, fail)
	shortenStoreSkip(t, 10*time.Millisecond)

	svc := twoStores(t)
	for round := 1; round <= storeFailureThreshold+1; round++ {
		svc.tick()
	}

	blocked := hits("R001")
	fail["R001"] = false              // 门店恢复
	time.Sleep(50 * time.Millisecond) // 等退避窗口过去

	svc.tick()
	if hits("R001") == blocked {
		t.Fatal("退避窗口过后应重新查询该门店，实际仍被跳过")
	}

	svc.tick()
	if item := svc.GetListenItems()["R001.P1"]; item.Status != StatusOutStock {
		t.Errorf("门店恢复后状态应回到 %q，实际 %q", StatusOutStock, item.Status)
	}
}

// 下一轮的预计时间要能被界面读到，否则退避时用户只看到一个不动的时间戳
func TestNextCheckIsExposed(t *testing.T) {
	stubStores(t, nil)

	svc := twoStores(t)
	if !svc.NextCheck().IsZero() {
		t.Fatal("尚未安排下一轮时应为零值")
	}

	svc.scheduleNext(30 * time.Second)

	left := time.Until(svc.NextCheck())
	if left <= 25*time.Second || left > 30*time.Second {
		t.Fatalf("距下一轮应约 30 秒，实际 %v", left)
	}
}
