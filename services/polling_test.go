package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"apple-store-helper/model"
)

// 抖动必须落在 ±jitterRatio 之内，否则要么过密要么过疏
func TestWithJitterStaysInRange(t *testing.T) {
	base := 10 * time.Second
	low := time.Duration(float64(base) * (1 - jitterRatio))
	high := time.Duration(float64(base) * (1 + jitterRatio))

	sawDifferent := false
	prev := withJitter(base)

	for i := 0; i < 200; i++ {
		got := withJitter(base)
		if got < low || got > high {
			t.Fatalf("抖动超出范围: %v 不在 [%v, %v]", got, low, high)
		}
		if got != prev {
			sawDifferent = true
		}
	}

	if !sawDifferent {
		t.Error("抖动没有产生变化，多个客户端仍会对齐到同一时刻")
	}
}

// 成功时应回到基础间隔附近
func TestNextDelayUsesBaseIntervalOnSuccess(t *testing.T) {
	svc := newListenService()
	svc.SetInterval(10 * time.Second)

	for i := 0; i < 20; i++ {
		got := svc.nextDelay(false)
		if got < 7*time.Second || got > 13*time.Second {
			t.Fatalf("成功时的间隔应在基础值附近，实际 %v", got)
		}
	}
}

// 连续失败必须退避：接口已经不可用或正在限流时，继续原频率敲门毫无意义
func TestNextDelayBacksOffOnFailure(t *testing.T) {
	svc := newListenService()
	svc.SetInterval(2 * time.Second)

	first := svc.nextDelay(true)
	second := svc.nextDelay(true)
	third := svc.nextDelay(true)

	if !(second > first && third > second) {
		t.Errorf("连续失败应逐次退避，实际 %v -> %v -> %v", first, second, third)
	}
}

func TestNextDelayCapsBackoff(t *testing.T) {
	svc := newListenService()
	svc.SetInterval(MinInterval)

	var got time.Duration
	for i := 0; i < 50; i++ {
		got = svc.nextDelay(true)
	}

	limit := time.Duration(float64(maxBackoff) * (1 + jitterRatio))
	if got > limit {
		t.Errorf("退避应有上限，实际 %v 超过 %v", got, limit)
	}
}

// 恢复正常后必须立刻回到基础间隔，而不是继续用退避后的长间隔
func TestNextDelayResetsAfterSuccess(t *testing.T) {
	svc := newListenService()
	svc.SetInterval(2 * time.Second)

	for i := 0; i < 6; i++ {
		svc.nextDelay(true)
	}

	got := svc.nextDelay(false)
	if got > 3*time.Second {
		t.Errorf("成功后应立刻恢复基础间隔，实际 %v", got)
	}
}

// 间隔不允许低于下限，否则「激进」会变成自残
func TestSetIntervalClampsToMinimum(t *testing.T) {
	svc := newListenService()

	svc.SetInterval(10 * time.Millisecond)
	if got := svc.GetInterval(); got != MinInterval {
		t.Errorf("过小的间隔应被抬到 %v，实际 %v", MinInterval, got)
	}

	svc.SetInterval(30 * time.Second)
	if got := svc.GetInterval(); got != 30*time.Second {
		t.Errorf("合法间隔不应被修改，实际 %v", got)
	}
}

// 门店多时不应一次性打出几十个连接
func TestGroupByStoreLimitsConcurrency(t *testing.T) {
	var mu sync.Mutex
	current, peak := 0, 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current++
		if current > peak {
			peak = current
		}
		mu.Unlock()

		time.Sleep(30 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()

		_, _ = w.Write([]byte(newShapeBody))
	}))
	defer srv.Close()

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	defer func() { pickupBaseURL = orig }()

	// 20 个不同门店
	items := map[string]ListenItem{}
	for i := 0; i < 20; i++ {
		store := model.Store{StoreNumber: fmt.Sprintf("R%03d", i), CityStoreName: fmt.Sprintf("门店%d", i)}
		product := model.Product{Code: "MJYH4CH/A", Title: "test"}
		items[store.StoreNumber+"."+product.Code] = ListenItem{Store: store, Product: product}
	}

	svc := newListenService()
	svc.SetArea(model.Areas[0])
	svc.groupByStore(items)

	mu.Lock()
	defer mu.Unlock()
	if peak > maxConcurrentRequests {
		t.Errorf("并发请求数应不超过 %d，实际峰值 %d", maxConcurrentRequests, peak)
	}
	if peak == 0 {
		t.Error("没有发出任何请求")
	}
	t.Logf("20 个门店，并发峰值 %d", peak)
}
