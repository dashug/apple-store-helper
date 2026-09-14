package services

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"apple-store-helper/model"
)

func addTwo(t *testing.T, svc *listenService) []string {
	t.Helper()

	area := model.Areas[0]
	svc.SetArea(area)

	stores := Store.ByAreaTitleForOptions(area.Title)
	products := Product.ByAreaTitleForOptions(area.Title)
	if _, err := svc.AddMany(area.Title, stores[:2], products[:1]); err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0, 2)
	for _, row := range svc.SortedRows() {
		keys = append(keys, row.Key)
	}
	return keys
}

// 停用项仍留在列表里，否则无法再启用
func TestDisabledItemStaysVisible(t *testing.T) {
	svc := newListenService()
	keys := addTwo(t, svc)

	svc.SetDisabled(keys[0], true)

	if got := len(svc.SortedRows()); got != 2 {
		t.Errorf("停用后仍应显示 2 行，实际 %d", got)
	}
}

// 停用项显示「已停用」而不是旧状态 —— 它不再被查询，旧状态是过期信息
func TestDisabledShowsDedicatedStatus(t *testing.T) {
	svc := newListenService()
	keys := addTwo(t, svc)

	svc.UpdateStatus(keys[0], StatusOutStock, "")
	svc.SetDisabled(keys[0], true)

	for _, row := range svc.SortedRows() {
		if row.Key != keys[0] {
			continue
		}
		if got := row.DisplayStatus(); got != StatusDisabled {
			t.Errorf("停用项应显示 %q，实际 %q", StatusDisabled, got)
		}
	}
}

// 停用项排在最后：它们不在监听中，不该占据视线焦点
func TestDisabledSinksToBottom(t *testing.T) {
	svc := newListenService()
	keys := addTwo(t, svc)

	svc.SetDisabled(keys[0], true)
	svc.UpdateStatus(keys[1], StatusOutStock, "")

	rows := svc.SortedRows()
	if rows[len(rows)-1].Key != keys[0] {
		t.Errorf("停用项应排在最后，实际最后一行是 %s", rows[len(rows)-1].Key)
	}
}

// 核心：停用项不再被查询
func TestDisabledItemIsNotQueried(t *testing.T) {
	// 门店查询是并发发出的，计数必须加锁，否则用例本身就有数据竞争
	var (
		mu      sync.Mutex
		queried int
	)
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return queried
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queried++
		mu.Unlock()

		_, _ = w.Write([]byte(newShapeBody))
	}))
	defer srv.Close()

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	defer func() { pickupBaseURL = orig }()

	svc := newListenService()
	keys := addTwo(t, svc)
	if err := svc.Status.Set(Running); err != nil {
		t.Fatal(err)
	}

	// 两项都启用：两个门店各查一次
	svc.tick()
	afterBoth := count()

	// 停用其中一项后，只应查一次
	mu.Lock()
	queried = 0
	mu.Unlock()

	svc.SetDisabled(keys[0], true)
	svc.tick()

	if afterBoth != 2 {
		t.Fatalf("两项启用时应查询 2 个门店，实际 %d", afterBoth)
	}
	if got := count(); got != 1 {
		t.Errorf("停用一项后应只查询 1 个门店，实际 %d", got)
	}
}

// 全部停用时不应发起任何查询
func TestAllDisabledSkipsRound(t *testing.T) {
	var (
		mu      sync.Mutex
		queried int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queried++
		mu.Unlock()
	}))
	defer srv.Close()

	orig := pickupBaseURL
	pickupBaseURL = srv.URL
	defer func() { pickupBaseURL = orig }()

	svc := newListenService()
	keys := addTwo(t, svc)
	if err := svc.Status.Set(Running); err != nil {
		t.Fatal(err)
	}

	for _, key := range keys {
		svc.SetDisabled(key, true)
	}

	if failed := svc.tick(); failed {
		t.Error("全部停用时不应判定为失败")
	}
	mu.Lock()
	got := queried
	mu.Unlock()

	if got != 0 {
		t.Errorf("全部停用时不应发起查询，实际 %d 次", got)
	}
}

func TestEnableRestoresMonitoring(t *testing.T) {
	svc := newListenService()
	keys := addTwo(t, svc)

	svc.SetDisabled(keys[0], true)
	if svc.ActiveCount() != 1 || svc.DisabledCount() != 1 {
		t.Fatalf("停用后计数不对：在监听 %d，已停用 %d", svc.ActiveCount(), svc.DisabledCount())
	}

	svc.SetDisabled(keys[0], false)
	if svc.ActiveCount() != 2 || svc.DisabledCount() != 0 {
		t.Errorf("恢复后计数不对：在监听 %d，已停用 %d", svc.ActiveCount(), svc.DisabledCount())
	}
}

// 全部停用时不应把界面标成「整体不可信」
func TestAllUnknownIgnoresDisabled(t *testing.T) {
	svc := newListenService()
	keys := addTwo(t, svc)

	svc.UpdateStatus(keys[0], StatusUnknown, "超时")
	svc.UpdateStatus(keys[1], StatusOutStock, "")
	svc.SetDisabled(keys[1], true)

	// 剩下的唯一在监听项确实是未知
	if !svc.AllUnknown() {
		t.Error("忽略停用项后应判定为整体不可信")
	}
}

// 停用状态要能持久化，否则重启后停用的项又开始查询
func TestDisabledSurvivesSettingsRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	svc := newListenService()
	keys := addTwo(t, svc)
	svc.SetDisabled(keys[0], true)

	if err := SaveSettings(UserSettings{ListenItems: svc.GetListenItems()}); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ListenItems[keys[0]].Disabled {
		t.Error("停用状态未持久化")
	}
	if loaded.ListenItems[keys[1]].Disabled {
		t.Error("未停用的项不应被标记为停用")
	}
}
