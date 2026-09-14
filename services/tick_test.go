package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"apple-store-helper/model"
	"apple-store-helper/view"
)

// 命中有货时会弹窗、发系统通知、打开购物袋，而 tick 运行在监听 goroutine 中。
// 这是整个程序里最关键也最少被走到的一条路径，这里完整跑一遍。
func TestTickHandlesInStockFromGoroutine(t *testing.T) {
	// 命中会写入有货记录，不隔离的话会写进使用者真实的配置目录
	withTempConfigDir(t)

	test.NewApp()
	view.App = fyne.CurrentApp()
	view.Window = view.App.NewWindow("test")

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
		},
	})
	svc.SetStatus(Running)

	// 在 goroutine 中执行，与生产环境一致
	done := make(chan bool, 1)
	go func() {
		done <- svc.tick()
	}()

	select {
	case failed := <-done:
		if failed {
			t.Errorf("本轮不应判定为失败: %+v", svc.SortedRows())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("tick 未返回，可能在等待主线程时死锁")
	}

	if got := svc.GetListenItems()[key].Status; got != StatusInStock {
		t.Errorf("应标记为有货，实际 %q", got)
	}

	// 命中后应自动暂停，避免反复弹窗
	status := svc.GetStatus()
	if status != Pause {
		t.Errorf("命中后应暂停，实际 %q", status)
	}

	// 命中必须被记进有货记录，否则「该盯哪家店」的判断依据就丢了
	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("读取有货记录失败: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("命中应产生 1 条记录，实际 %d", len(entries))
	}
	if entries[0].Store != "上海-环球港" {
		t.Errorf("记录的门店不对: %q", entries[0].Store)
	}
}

// 查询失败时不应标记为无货，也不应弹窗
func TestTickMarksUnknownOnFailure(t *testing.T) {
	withTempConfigDir(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(541)
		_, _ = w.Write([]byte("<!doctype html><html><body>blocked</body></html>"))
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
		},
	})
	svc.SetStatus(Running)

	done := make(chan bool, 1)
	go func() { done <- svc.tick() }()

	select {
	case failed := <-done:
		if !failed {
			t.Error("被拦截时本轮应判定为失败，以便触发退避")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("tick 未返回")
	}

	item := svc.GetListenItems()[key]
	if item.Status != StatusUnknown {
		t.Errorf("被拦截时应标记为未知而非无货，实际 %q", item.Status)
	}
	if item.Detail == "" {
		t.Error("未知状态应带上原因")
	}
}
