package services

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"apple-store-helper/model"
)

func testItem() ListenItem {
	return ListenItem{
		Store:   model.Store{StoreNumber: "R683", CityStoreName: "上海-环球港"},
		Product: model.Product{Code: "MYEV3CH/A", Title: "iPhone 17 Pro - 深蓝色 - 256GB"},
		Status:  StatusWait,
	}
}

// 监听 goroutine 与 UI 线程会同时访问 listenService 的共享状态，
// 本用例需在 -race 下运行才有意义。
func TestListenServiceConcurrentAccess(t *testing.T) {
	svc := newListenService()
	const key = "R683.MYEV3CH/A"

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 监听 goroutine：不断改状态、刷日志、读地区
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				svc.UpdateStatus(key, StatusOutStock, "")
				svc.UpdateLogStr()
				_ = svc.GetArea().ShortCode
				_ = svc.GetBarkNotifyUrl()
			}
		}
	}()

	// UI 线程：添加、读取待保存项、清空、切地区、改 Bark 地址
	for i := 0; i < 2000; i++ {
		svc.SetListenItems(map[string]ListenItem{key: testItem()})
		_ = svc.GetListenItems()
		svc.SetArea(model.Areas[i%len(model.Areas)])
		svc.SetBarkNotifyUrl("https://api.day.app/key")
		svc.Clean()
	}

	close(stop)
	wg.Wait()
}

// GetListenItems 返回的必须是快照，调用方持有后不应再受监听 goroutine 影响
func TestGetListenItemsReturnsSnapshot(t *testing.T) {
	svc := newListenService()
	const key = "R683.MYEV3CH/A"
	svc.SetListenItems(map[string]ListenItem{key: testItem()})

	snapshot := svc.GetListenItems()
	svc.Clean()

	if len(snapshot) != 1 {
		t.Fatalf("快照被 Clean 影响，期望 1 项，实际 %d 项", len(snapshot))
	}
	if len(svc.GetListenItems()) != 0 {
		t.Fatal("Clean 之后监听列表应为空")
	}
}

// 监听项已被「清空」移除时，UpdateStatus 不应把空壳条目写回
func TestUpdateStatusIgnoresRemovedItem(t *testing.T) {
	svc := newListenService()
	svc.UpdateStatus("不存在的 key", StatusOutStock, "")

	if got := len(svc.GetListenItems()); got != 0 {
		t.Fatalf("不应写回已移除的条目，实际残留 %d 项", got)
	}
}

// 门店/型号列表变化后，添加旧的监听项应报错而不是 panic
func TestAddUnknownStoreReturnsError(t *testing.T) {
	svc := newListenService()

	if err := svc.Add("中国大陆", "不存在的门店", "不存在的型号"); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if got := len(svc.GetListenItems()); got != 0 {
		t.Fatalf("失败的添加不应写入监听列表，实际 %d 项", got)
	}
}

func TestGetStoreUnknownReturnsError(t *testing.T) {
	Store.ByArea(model.Areas[0]) // 预填充门店

	if _, err := Store.GetStore("中国大陆", "不存在的门店"); err == nil {
		t.Fatal("门店不存在时期望返回错误")
	}
	if _, err := Store.GetStore("不存在的地区", "上海-环球港"); err == nil {
		t.Fatal("地区不存在时期望返回错误")
	}
}

func TestGetProductUnknownReturnsError(t *testing.T) {
	if _, err := Product.GetProduct("中国大陆", "不存在的型号"); err == nil {
		t.Fatal("型号不存在时期望返回错误")
	}
	if _, err := Product.GetProduct("不存在的地区", "iPhone 17"); err == nil {
		t.Fatal("地区不存在时期望返回错误")
	}
}

// 网络不可达时只记日志，不能让整个程序崩溃
func TestBarkNetworkErrorDoesNotPanic(t *testing.T) {
	svc := newListenService()
	svc.SetBarkNotifyUrl("http://127.0.0.1:1/bark-key")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Bark 推送失败不应 panic: %v", r)
		}
	}()

	svc.SendPushNotificationByBark("有货提醒", "测试", "https://www.apple.com/cn/shop/bag")
}

// 未填写 Bark 地址（含只输了空白）时应直接跳过，不发请求
func TestBarkEmptyUrlIsNoop(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	svc := newListenService()
	for _, notifyUrl := range []string{"", "   ", "\t\n"} {
		svc.SetBarkNotifyUrl(notifyUrl)
		svc.SendPushNotificationByBark("有货提醒", "测试", "https://www.apple.com/cn/shop/bag")
	}

	if hits != 0 {
		t.Fatalf("未配置 Bark 地址时不应发起请求，实际发起 %d 次", hits)
	}
}

// 标题/正文进入 URL path，含 ? # 等字符时必须转义，否则 url 参数会被破坏
func TestBarkEscapesTitleAndContent(t *testing.T) {
	const (
		title   = "有货提醒"
		content = "上海-环球港 iPhone 17 有货?真的#有货"
		bagUrl  = "https://www.apple.com/cn/shop/bag"
	)

	var gotPath, gotBagUrl string
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBagUrl = r.URL.Query().Get("url")
		close(done)
	}))
	defer srv.Close()

	svc := newListenService()
	svc.SetBarkNotifyUrl(srv.URL + "/bark-key/")
	svc.SendPushNotificationByBark(title, content, bagUrl)
	<-done

	if want := "/bark-key/" + title + "/" + content; gotPath != want {
		t.Errorf("path 不正确\n期望: %s\n实际: %s", want, gotPath)
	}
	if gotBagUrl != bagUrl {
		t.Errorf("url 参数不正确\n期望: %s\n实际: %s", bagUrl, gotBagUrl)
	}
}
