package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"apple-store-helper/model"
)

// 命中时的呈现交给调用方，services 只负责把事件送出去
func TestOnInStockReceivesEvent(t *testing.T) {
	withTempConfigDir(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newShapeBody))
	}))
	defer srv.Close()

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	defer func() { pickupBaseURL = orig }()

	svc := newListenService()
	svc.SetArea(model.Areas[0])

	const key = "R683.MJYH4CH/A"
	svc.SetListenItems(map[string]ListenItem{
		key: {
			Store:   model.Store{StoreNumber: "R683", CityStoreName: "上海-环球港"},
			Product: model.Product{Code: "MJYH4CH/A", Title: "iPhone 18 Pro Max"},
			Status:  StatusWait,
			Area:    model.Areas[0].Title,
		},
	})
	svc.SetStatus(Running)

	var (
		mu    sync.Mutex
		event InStockEvent
		fired bool
	)
	svc.SetOnInStock(func(e InStockEvent) {
		mu.Lock()
		event, fired = e, true
		mu.Unlock()
	})

	svc.tick()

	mu.Lock()
	defer mu.Unlock()

	if !fired {
		t.Fatal("命中时应触发回调")
	}
	if len(event.Items) != 1 {
		t.Fatalf("应有 1 项命中，实际 %d", len(event.Items))
	}
	if event.Items[0].Store.CityStoreName != "上海-环球港" {
		t.Errorf("事件里的门店不对: %+v", event.Items[0].Store)
	}
	if !strings.Contains(event.Message, "有货") {
		t.Errorf("提示文案不对: %q", event.Message)
	}
	if !strings.Contains(event.BagURL, "/shop/bag") {
		t.Errorf("购物袋地址不对: %q", event.BagURL)
	}
}

// 未注册回调时不应 panic —— 命令行形态可能根本不需要它
func TestTickWithoutInStockHandler(t *testing.T) {
	withTempConfigDir(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newShapeBody))
	}))
	defer srv.Close()

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	defer func() { pickupBaseURL = orig }()

	svc := newListenService()
	svc.SetArea(model.Areas[0])
	svc.SetListenItems(map[string]ListenItem{
		"R683.MJYH4CH/A": {
			Store:   model.Store{StoreNumber: "R683", CityStoreName: "上海-环球港"},
			Product: model.Product{Code: "MJYH4CH/A", Title: "iPhone 18 Pro Max"},
			Area:    model.Areas[0].Title,
		},
	})
	svc.SetStatus(Running)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("未注册回调时不应 panic: %v", r)
		}
	}()

	svc.tick()
}

// 状态改由普通值承载，切换时要通知界面刷新
func TestSetStatusNotifiesOnChange(t *testing.T) {
	svc := newListenService()

	calls := 0
	svc.SetOnChange(func() { calls++ })

	svc.SetStatus(Running)
	if calls != 1 {
		t.Errorf("状态变化应触发 1 次刷新，实际 %d", calls)
	}

	// 设成相同的值不应重复刷新
	svc.SetStatus(Running)
	if calls != 1 {
		t.Errorf("状态未变化时不应刷新，实际累计 %d 次", calls)
	}

	svc.SetStatus(Pause)
	if calls != 2 {
		t.Errorf("状态再次变化应触发刷新，实际累计 %d 次", calls)
	}
}

func TestNewServiceStartsPaused(t *testing.T) {
	if got := newListenService().GetStatus(); got != Pause {
		t.Errorf("初始状态应为 %q，实际 %q", Pause, got)
	}
}
