package services

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-module/carbon"
	"github.com/tidwall/gjson"

	"apple-store-helper/model"
)

const (
	StatusOutStock = "无货"
	StatusInStock  = "有货"
	StatusWait     = "等待"
	// StatusUnknown 表示这一轮没能问出结果（被拦截、超时、结构变化等）。
	// 它不等于无货 —— 把查询失败显示成「无货」会让用户以为真没货而放弃。
	StatusUnknown = "未知"

	Pause   = "暂停"
	Running = "监听中"
)

// pickupPath 是当前可用的库存查询接口。
//
// 原先使用的 /shop/fulfillment-messages 现在对任意请求恒定返回 HTTP 541
// 加一个拦截页，已完全不可用。/shop/retail/pickup-message 接受相同的
// parts.N / store 参数，且 messageTypes.compact.storeSelectionEnabled
// 的语义未变。
const pickupPath = "shop/retail/pickup-message"

const (
	// DefaultInterval 是两轮库存检查之间的基础间隔。
	//
	// 原先固定 500ms，对 Apple 接口过于激进：门店越多，单位时间内的
	// 请求越密，高峰期正是最容易被限流的时候，而限流的结果就是一屏
	// 「未知」—— 恰好在最需要准确结果的时刻失去准确结果。
	DefaultInterval = 5 * time.Second

	// MinInterval 是允许设置的最小间隔
	MinInterval = 2 * time.Second

	// maxBackoff 是连续失败后的退避上限
	maxBackoff = 5 * time.Minute

	// jitterRatio 是叠加在间隔上的随机抖动比例。
	// 固定周期会让多个用户的请求逐渐对齐到同一时刻，反而更容易触发限流。
	jitterRatio = 0.25

	// maxConcurrentRequests 限制同时在飞的请求数。
	// 原先所有门店一次性并发，盯二十家门店就会瞬间打出二十个连接。
	maxConcurrentRequests = 4

	// pausedPollInterval 是暂停状态下的空转间隔，只为及时响应「开始」
	pausedPollInterval = 200 * time.Millisecond
)

// pickupBaseURL 是站点根地址，测试中会被替换成本地服务
var pickupBaseURL = "https://www.apple.com"

// pickupClient 复用连接。
// 原先每次请求都新建客户端，意味着每轮、每个门店都要重新握手一次 TLS。
var pickupClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	},
}

var Listen = newListenService()

func newListenService() *listenService {
	return &listenService{
		items:    map[string]ListenItem{},
		status:   Pause,
		area:     model.Areas[0],
		interval: DefaultInterval,
	}
}

// listenService 的可变状态同时被 UI 线程和监听 goroutine 访问，一律经 mu 保护。
// 取值/赋值请走 GetXxx / SetXxx，不要直接读写字段。
type listenService struct {
	mu            sync.RWMutex
	items         map[string]ListenItem
	area          model.Area
	barkNotifyUrl string
	notifyUrls    string

	// status 是监听状态。此前用 fyne 的 data binding，那让 services 绑死在
	// GUI 上 —— 链接了 fyne 的二进制在裸服务器上会因为缺 libGL/libX11
	// 根本起不来，无法做 headless 运行。
	status string

	// interval 是基础轮询间隔，failures 是连续失败轮次（用于退避）
	interval time.Duration
	failures int

	// lastCheck 是上一轮检查完成的时间。
	// 界面此前只有「暂停 / 监听中」，看不出程序是否还在正常轮转，
	// 用户只能盯着列表里的时间列变化来判断。
	lastCheck carbon.DateTime

	// onInStock 在命中有货时被调用，由调用方决定如何呈现
	onInStock func(InStockEvent)

	// onChange 在监听列表发生变化后被调用，供界面刷新列表。
	// 只在 UI 初始化时设置一次，读写仍受 mu 保护。
	onChange func()
}

// ListenRow 是展示用的一行。Key 用于定位与删除。
type ListenRow struct {
	ListenItem
	Key string
}

// SetStatus 设置监听状态并通知界面刷新
func (s *listenService) SetStatus(status string) {
	s.mu.Lock()
	changed := s.status != status
	s.status = status
	s.mu.Unlock()

	if changed {
		s.notifyChange()
	}
}

func (s *listenService) GetStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// InStockEvent 描述一次命中。
//
// 弹窗、打开购物袋、播放提示音都是界面行为，交由调用方处理：
// GUI 注册自己的实现，命令行注册打印日志的实现，services 本身不碰 GUI。
type InStockEvent struct {
	Item    ListenItem
	Message string
	BagURL  string
}

// SetOnInStock 注册命中时的处理
func (s *listenService) SetOnInStock(fn func(InStockEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onInStock = fn
}

// SetOnChange 注册列表变化的回调
func (s *listenService) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// StatusDisabled 是停用项的展示文案。
// 停用项不再被查询，沿用旧状态会显示成「无货」，那是过期且误导的信息。
const StatusDisabled = "已停用"

// statusRank 决定行的排序优先级：
// 有货最前，其次是需要留意的「未知」，停用的沉到最后 —— 它们不在监听中
func statusRank(status string) int {
	switch status {
	case StatusInStock:
		return 0
	case StatusUnknown:
		return 1
	case StatusWait:
		return 2
	case StatusDisabled:
		return 4
	default:
		return 3
	}
}

// DisplayStatus 返回该行应显示的状态
func (r ListenRow) DisplayStatus() string {
	if r.Disabled {
		return StatusDisabled
	}
	return r.Status
}

// SortedRows 返回展示用的有序快照。
// 顺序必须稳定，否则列表会在每轮刷新时乱跳，用户根本点不中删除按钮。
func (s *listenService) SortedRows() []ListenRow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	area := s.area.Title
	rows := make([]ListenRow, 0, len(s.items))
	for key, item := range s.items {
		if item.Area != area {
			continue
		}
		rows = append(rows, ListenRow{ListenItem: item, Key: key})
	}

	sort.Slice(rows, func(i, j int) bool {
		ri, rj := statusRank(rows[i].DisplayStatus()), statusRank(rows[j].DisplayStatus())
		if ri != rj {
			return ri < rj
		}
		if rows[i].Store.CityStoreName != rows[j].Store.CityStoreName {
			return rows[i].Store.CityStoreName < rows[j].Store.CityStoreName
		}
		if rows[i].Product.Title != rows[j].Product.Title {
			return rows[i].Product.Title < rows[j].Product.Title
		}
		return rows[i].Key < rows[j].Key
	})

	return rows
}

// AllUnknown 表示这一轮所有监听项都没问出结果，整体不可信
func (s *listenService) AllUnknown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	area := s.area.Title
	seen := false

	for _, item := range s.items {
		if item.Area != area || item.Disabled {
			continue
		}
		seen = true
		if item.Status != StatusUnknown {
			return false
		}
	}

	return seen
}

// SetDisabled 停用或恢复单个监听项
func (s *listenService) SetDisabled(key string, disabled bool) {
	s.mu.Lock()
	if item, ok := s.items[key]; ok {
		item.Disabled = disabled
		s.items[key] = item
	}
	fn := s.onChange
	s.mu.Unlock()

	if fn != nil {
		fn()
	}
}

// Remove 删除单个监听项。此前只能整体「清空」，加错一条就得全部重来。
func (s *listenService) Remove(key string) {
	s.mu.Lock()
	delete(s.items, key)
	fn := s.onChange
	s.mu.Unlock()

	if fn != nil {
		fn()
	}
}

func (s *listenService) GetArea() model.Area {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.area
}

// SetArea 切换当前地区。
// 各地区的监听列表分开保存，切换只改变显示与监听的范围，不会清空任何一边。
func (s *listenService) SetArea(area model.Area) {
	s.mu.Lock()
	s.area = area
	s.mu.Unlock()

	// 可见的监听项随之变化，通知界面刷新
	s.notifyChange()
}

func (s *listenService) SetBarkNotifyUrl(notifyUrl string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.barkNotifyUrl = notifyUrl
}

func (s *listenService) GetBarkNotifyUrl() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.barkNotifyUrl
}

// SetNotifyUrls 设置除 Bark 之外的通知地址，每行一个
func (s *listenService) SetNotifyUrls(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifyUrls = raw
}

func (s *listenService) GetNotifyUrls() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notifyUrls
}

// NotifyTargets 返回全部已配置的通知地址。
//
// Bark 保留独立字段：它支持自建，按域名识别会把自建地址误判成普通 Webhook，
// 因此不与其他渠道混在一起。
func (s *listenService) NotifyTargets() []NotifyTarget {
	s.mu.RLock()
	bark := strings.TrimSpace(s.barkNotifyUrl)
	extra := s.notifyUrls
	s.mu.RUnlock()

	var targets []NotifyTarget
	if bark != "" {
		// 显式标注为 Bark：自建 Bark 的域名不是 day.app，靠识别会被误判
		targets = append(targets, NotifyTarget{URL: bark, Channel: ChannelBark})
	}

	return append(targets, splitTargets(extra)...)
}

// Notify 向所有已配置的渠道发送提醒，逐条返回结果。
// 逐条返回是为了让界面能指出是哪个渠道失败了。
func (s *listenService) Notify(n Notification) []NotifyResult {
	targets := s.NotifyTargets()
	results := make([]NotifyResult, 0, len(targets))

	for _, target := range targets {
		result := sendOne(target, n)
		if result.Err != nil {
			log.Printf("%s 通知发送失败: %v", result.Channel, result.Err)
		}
		results = append(results, result)
	}

	return results
}

type ListenItem struct {
	Store   model.Store
	Product model.Product
	Status  string
	Time    carbon.DateTime

	// Disabled 表示暂时不监听这一项。
	// 此前只能删除，想暂时不盯某家店就得删了重加，等于丢掉配置。
	Disabled bool `json:"disabled"`

	// Area 是该监听项所属地区。
	// 各地区的列表分开保存，切换地区只是切换显示与监听的范围，
	// 不再清空 —— 误点一下地区就丢掉全部配置，且不可恢复。
	Area string `json:"area"`

	// Detail 是「未知」状态的原因，仅用于展示，不写入配置文件
	Detail string `json:"-"`
}

// storeResult 是一次门店查询的结果。
// err 非空时 skus 没有意义 —— 调用方必须把这些型号标记为「未知」而不是「无货」。
type storeResult struct {
	storeNumber string
	skus        map[string]bool
	err         error
}

// Add 添加单个监听项
func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string) error {
	_, err := s.AddMany(areaTitle, []string{storeTitle}, []string{productTitle})
	return err
}

// AddMany 批量添加「所选门店 × 所选型号」的全部组合，返回新增条数。
//
// 先把门店与型号全部解析完再写入：只要有一个解析不出来就整体失败，
// 避免用户以为加了 20 条、实际只加进去 12 条。
func (s *listenService) AddMany(areaTitle string, storeTitles []string, productTitles []string) (int, error) {
	stores := make([]model.Store, 0, len(storeTitles))
	for _, title := range storeTitles {
		store, err := Store.GetStore(areaTitle, title)
		if err != nil {
			return 0, err
		}
		stores = append(stores, store)
	}

	products := make([]model.Product, 0, len(productTitles))
	for _, title := range productTitles {
		product, err := Product.GetProduct(areaTitle, title)
		if err != nil {
			return 0, err
		}
		products = append(products, product)
	}

	added := 0

	s.mu.Lock()
	for _, store := range stores {
		for _, product := range products {
			uniqKey := store.StoreNumber + "." + product.Code
			// 已在监听中的组合不重复添加，也不重置它的状态
			if s.items[uniqKey].Store.StoreNumber != "" {
				continue
			}
			s.items[uniqKey] = ListenItem{
				Store:   store,
				Product: product,
				Status:  StatusWait,
				Area:    areaTitle,
			}
			added++
		}
	}
	fn := s.onChange
	s.mu.Unlock()

	if fn != nil {
		fn()
	}

	return added, nil
}

// Clean 清空当前地区的监听列表。
// 只清当前地区：界面上显示的就是这一部分，清掉看不见的其他地区会让人意外。
func (s *listenService) Clean() {
	s.mu.Lock()
	area := s.area.Title
	for key, item := range s.items {
		if item.Area == area {
			delete(s.items, key)
		}
	}
	s.mu.Unlock()

	s.notifyChange()
}

// CleanAll 清空全部地区，供「清空」之外的场景使用
func (s *listenService) CleanAll() {
	s.mu.Lock()
	s.items = map[string]ListenItem{}
	s.mu.Unlock()

	s.notifyChange()
}

func (s *listenService) SetListenItems(items map[string]ListenItem) {
	s.mu.Lock()
	// 拷贝一份，避免与调用方共享底层 map
	s.items = make(map[string]ListenItem, len(items))
	for k, v := range items {
		// 旧配置文件里没有 Area 字段，归入当前地区，否则升级后列表会整个消失
		if v.Area == "" {
			v.Area = s.area.Title
		}
		s.items[k] = v
	}
	s.mu.Unlock()

	s.notifyChange()
}

// GetListenItems 返回全部地区的快照，用于持久化。
// 监听与展示请用 CurrentAreaItems —— 否则会把其他地区的型号拿去当前地区查询。
func (s *listenService) GetListenItems() map[string]ListenItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make(map[string]ListenItem, len(s.items))
	for k, v := range s.items {
		items[k] = v
	}

	return items
}

// ActiveCount 返回当前地区正在监听的项数（不含停用）
func (s *listenService) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	area := s.area.Title
	count := 0
	for _, item := range s.items {
		if item.Area == area && !item.Disabled {
			count++
		}
	}

	return count
}

// DisabledCount 返回当前地区被停用的项数
func (s *listenService) DisabledCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	area := s.area.Title
	count := 0
	for _, item := range s.items {
		if item.Area == area && item.Disabled {
			count++
		}
	}

	return count
}

// CurrentAreaItems 返回当前地区的监听项快照
func (s *listenService) CurrentAreaItems() map[string]ListenItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	area := s.area.Title
	items := map[string]ListenItem{}
	for k, v := range s.items {
		if v.Area == area {
			items[k] = v
		}
	}

	return items
}

// notifyChange 通知界面刷新列表。回调在锁外执行，避免 UI 刷新与监听线程互等。
func (s *listenService) notifyChange() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()

	if fn != nil {
		fn()
	}
}

func (s *listenService) UpdateStatus(uniqKey string, status string, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 可能已被「清空」移除，此时不要再写回
	item, ok := s.items[uniqKey]
	if !ok {
		return
	}

	item.Time = carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
	item.Status = status
	item.Detail = detail
	s.items[uniqKey] = item
}

func (s *listenService) Run() {
	s.SetStatus(Pause)

	go func() {
		for {
			if s.GetStatus() != Running {
				time.Sleep(pausedPollInterval)
				continue
			}

			failed := s.tick()
			time.Sleep(s.nextDelay(failed))
		}
	}()
}

// SetInterval 设置基础轮询间隔，低于 MinInterval 的取值会被抬到下限
func (s *listenService) SetInterval(d time.Duration) {
	if d < MinInterval {
		d = MinInterval
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = d
}

func (s *listenService) GetInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interval
}

// nextDelay 计算下一轮的等待时间。
//
// 成功则回到基础间隔；连续失败时指数退避，避免在接口已经不可用或
// 正在限流我们的时候继续以原频率敲门。无论哪种情况都叠加抖动。
func (s *listenService) nextDelay(failed bool) time.Duration {
	s.mu.Lock()
	if failed {
		s.failures++
	} else {
		s.failures = 0
	}
	failures := s.failures
	base := s.interval
	s.mu.Unlock()

	delay := base
	if failures > 0 {
		shift := failures - 1
		if shift > 16 {
			shift = 16
		}
		delay = base * time.Duration(1<<shift)
		if delay > maxBackoff || delay <= 0 {
			delay = maxBackoff
		}
	}

	return withJitter(delay)
}

// withJitter 给间隔叠加 ±jitterRatio 的随机抖动
func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}

	delta := float64(d) * jitterRatio
	return time.Duration(float64(d) - delta + rand.Float64()*2*delta)
}

// tick 执行一轮库存检查，返回本轮是否有门店查询失败（用于退避）
func (s *listenService) tick() bool {
	if s.GetStatus() != Running {
		return false
	}

	// 整轮使用同一份快照，避免与 UI 线程的增删并发。
	// 只取当前地区：其他地区的货号在本地区的接口上查不到。
	items := map[string]ListenItem{}
	for key, item := range s.CurrentAreaItems() {
		if item.Disabled {
			continue
		}
		items[key] = item
	}
	if len(items) == 0 {
		return false
	}

	skus, failures := s.groupByStore(items)

	for key, item := range items {
		// 查询失败的门店一律标记为「未知」，绝不能当成无货
		if reason, failed := failures[item.Store.StoreNumber]; failed {
			s.UpdateStatus(key, StatusUnknown, reason)
			continue
		}

		if !skus[item.Store.StoreNumber+"."+item.Product.Code] {
			s.UpdateStatus(key, StatusOutStock, "")
			continue
		}

		s.UpdateStatus(key, StatusInStock, "")
		s.SetStatus(Pause)

		// 记录这次命中。程序看到的每一轮结果原本都直接丢掉了，
		// 而「哪家店什么时候出过货」是别处拿不到的信息。
		RecordInStock(s.GetArea().Title, item)

		bagUrl := fmt.Sprintf("https://www.apple.com/%s/shop/bag", s.GetArea().ShortCode)
		msg := fmt.Sprintf("%s %s 有货", item.Store.CityStoreName, item.Product.Title)

		// 呈现方式交给调用方：GUI 弹窗并打开购物袋，命令行打印日志。
		// services 不再直接触碰 GUI，否则无法在无桌面环境运行。
		s.mu.RLock()
		onInStock := s.onInStock
		s.mu.RUnlock()

		if onInStock != nil {
			onInStock(InStockEvent{Item: item, Message: msg, BagURL: bagUrl})
		}

		// 推送与界面无关，各形态都需要
		go s.Notify(Notification{Title: "有货提醒", Content: msg, URL: bagUrl})
		break
	}

	s.mu.Lock()
	s.lastCheck = carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
	s.mu.Unlock()

	s.notifyChange()

	return len(failures) > 0
}

// LastCheck 返回上一轮检查完成的时间，零值表示尚未完成过任何一轮
func (s *listenService) LastCheck() carbon.DateTime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastCheck
}

// groupByStore 按门店合并查询，返回各 SKU 的有货情况，
// 以及查询失败的门店及其原因（门店号 -> 原因）
func (s *listenService) groupByStore(items map[string]ListenItem) (map[string]bool, map[string]string) {
	skus := map[string]bool{}
	failures := map[string]string{}

	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	group := map[string][]ListenItem{}
	reqs := map[string]string{}

	for _, item := range items {
		group[item.Store.StoreNumber] = append(group[item.Store.StoreNumber], item)
	}

	shortCode := s.GetArea().ShortCode

	for storeNumber, items := range group {

		var uri url.URL
		q := uri.Query()
		q.Set("little", "true")
		q.Set("mt", "regular")
		q.Set("store", storeNumber)

		for index, item := range items {
			q.Set("parts."+strconv.FormatInt(int64(index), 10), item.Product.Code)
		}

		queryStr := q.Encode()

		link := fmt.Sprintf(
			"%s/%s/%s?%s",
			pickupBaseURL,
			shortCode,
			pickupPath,
			queryStr,
		)

		reqs[storeNumber] = link
	}

	count := len(reqs)
	if count < 1 {
		return skus, failures
	}

	ch := make(chan storeResult, count)
	sem := make(chan struct{}, maxConcurrentRequests)

	for storeNumber, link := range reqs {
		go func(storeNumber, link string) {
			// 限制同时在飞的请求数，门店多时不至于瞬间打出几十个连接
			sem <- struct{}{}
			defer func() { <-sem }()

			ch <- fetchStore(storeNumber, link)
		}(storeNumber, link)
	}

	for i := 0; i < count; i++ {
		res := <-ch
		if res.err != nil {
			log.Printf("查询门店 %s 失败: %v", res.storeNumber, res.err)
			failures[res.storeNumber] = res.err.Error()
			continue
		}
		for key, v := range res.skus {
			skus[key] = v
		}
	}

	return skus, failures
}

// fetchStore 查询单个门店的库存。
//
// 任何「问不出结果」的情况都通过 err 返回，调用方据此标记为「未知」。
// 返回空结果而不报错，会让被拦截、超时、接口变更都伪装成「无货」。
func fetchStore(storeNumber string, skUrl string) storeResult {
	res := storeResult{storeNumber: storeNumber, skus: map[string]bool{}}

	req, err := http.NewRequest(http.MethodGet, skUrl, nil)
	if err != nil {
		res.err = fmt.Errorf("请求构造失败: %w", err)
		return res
	}
	req.Header.Set("referer", "https://www.apple.com/shop/buy-iphone")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := pickupClient.Do(req)
	if err != nil {
		res.err = fmt.Errorf("网络错误: %w", err)
		return res
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		res.err = fmt.Errorf("读取响应失败: %w", err)
		return res
	}
	body := string(raw)

	if resp.StatusCode != http.StatusOK {
		// 接口被拦截时会返回 HTTP 541 加一个 HTML 页面，
		// 不检查状态码就解析，会把拦截页解析成「所有型号无货」
		res.err = fmt.Errorf("接口返回 HTTP %d", resp.StatusCode)
		return res
	}

	stores := gjson.Get(body, "body.stores")
	if !stores.Exists() {
		// 兼容旧版的嵌套结构，以防 Apple 把数据挪回去
		stores = gjson.Get(body, "body.content.pickupMessage.stores")
	}

	if !stores.Exists() {
		if msg := gjson.Get(body, "body.errorMessage").String(); msg != "" {
			res.err = fmt.Errorf("接口报错: %s", msg)
		} else {
			res.err = errors.New("响应结构无法识别，接口可能已变更")
		}
		return res
	}

	found := false
	for _, store := range stores.Array() {
		for productCode, availability := range store.Get("partsAvailability").Map() {
			uniqKey := fmt.Sprintf("%s.%s", store.Get("storeNumber").String(), productCode)
			res.skus[uniqKey] = availability.Get("messageTypes.compact.storeSelectionEnabled").Bool()
			found = true
		}
	}

	// 响应合法但一条库存都没有，同样属于问不出结果
	if !found {
		res.err = errors.New("响应中没有门店库存数据")
		return res
	}

	return res
}
