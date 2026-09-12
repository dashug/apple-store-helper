package services

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/golang-module/carbon"
	"github.com/parnurzeal/gorequest"
	"github.com/tidwall/gjson"

	"apple-store-helper/model"
	"apple-store-helper/theme"
	"apple-store-helper/view"
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

var Listen = newListenService()

func newListenService() *listenService {
	return &listenService{
		items:  map[string]ListenItem{},
		Status: binding.NewString(),
		area:   model.Areas[0],
		Logs:   widget.NewLabel(""),
	}
}

// listenService 的可变状态同时被 UI 线程和监听 goroutine 访问，一律经 mu 保护。
// 取值/赋值请走 GetXxx / SetXxx，不要直接读写字段。
type listenService struct {
	mu            sync.RWMutex
	items         map[string]ListenItem
	area          model.Area
	barkNotifyUrl string

	Status binding.String
	Logs   *widget.Label
}

func (s *listenService) GetArea() model.Area {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.area
}

func (s *listenService) SetArea(area model.Area) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.area = area
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

type ListenItem struct {
	Store   model.Store
	Product model.Product
	Status  string
	Time    carbon.DateTime

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

func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string) error {

	store, err := Store.GetStore(areaTitle, storeTitle)
	if err != nil {
		return err
	}

	product, err := Product.GetProduct(areaTitle, productTitle)
	if err != nil {
		return err
	}

	uniqKey := store.StoreNumber + "." + product.Code

	s.mu.Lock()
	if s.items[uniqKey].Store.StoreNumber == "" {
		s.items[uniqKey] = ListenItem{
			Store:   store,
			Product: product,
			Status:  StatusWait,
		}
	}
	text := s.logText()
	s.mu.Unlock()

	s.Logs.SetText(text)
	return nil
}

func (s *listenService) Clean() {
	s.mu.Lock()
	s.items = map[string]ListenItem{}
	s.mu.Unlock()

	s.UpdateLogStr()
}

func (s *listenService) SetListenItems(items map[string]ListenItem) {
	s.mu.Lock()
	// 拷贝一份，避免与调用方共享底层 map
	s.items = make(map[string]ListenItem, len(items))
	for k, v := range items {
		s.items[k] = v
	}
	s.mu.Unlock()

	s.UpdateLogStr()
}

// GetListenItems 返回快照，调用方可以安全地遍历或序列化
func (s *listenService) GetListenItems() map[string]ListenItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make(map[string]ListenItem, len(s.items))
	for k, v := range s.items {
		items[k] = v
	}

	return items
}

func (s *listenService) UpdateLogStr() {
	s.mu.RLock()
	text := s.logText()
	s.mu.RUnlock()

	s.Logs.SetText(text)
}

// logText 拼接日志文本，调用方必须已持有 mu
func (s *listenService) logText() string {
	var str string
	unknown := 0

	for _, item := range s.items {
		detail := ""
		if item.Detail != "" {
			detail = "  (" + item.Detail + ")"
		}
		if item.Status == StatusUnknown {
			unknown++
		}

		str += fmt.Sprintf(
			"[%s] %s %s %s%s\n",
			item.Status,
			item.Time,
			item.Store.CityStoreName,
			item.Product.Title,
			detail,
		)
	}

	// 全部查询不到结果时，监听结果整体不可信，必须显著提示，
	// 否则用户会把一屏「未知」当成「都没货」
	if unknown > 0 && unknown == len(s.items) {
		str = "⚠️ 当前无法获取库存，以下结果均不可信（接口可能已变更或被限流）\n\n" + str
	}

	return str
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
	s.Status.Set(Pause)

	go func() {
		for {
			s.tick()
			time.Sleep(time.Millisecond * 500)
		}
	}()
}

// tick 执行一轮库存检查
func (s *listenService) tick() {
	if status, err := s.Status.Get(); err != nil || status != Running {
		return
	}

	// 整轮使用同一份快照，避免与 UI 线程的增删并发
	items := s.GetListenItems()
	if len(items) == 0 {
		return
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
		s.Status.Set(Pause)

		var bagUrl = fmt.Sprintf("https://www.apple.com/%s/shop/bag", s.GetArea().ShortCode)
		// 进入购物袋
		s.openBrowser(bagUrl)
		msg := fmt.Sprintf("%s %s 有货", item.Store.CityStoreName, item.Product.Title)
		dialog.ShowInformation("匹配成功", msg, view.Window)
		view.App.SendNotification(&fyne.Notification{
			Title:   "有货提醒",
			Content: msg,
		})
		go s.AlertMp3()
		go s.SendPushNotificationByBark("有货提醒", msg, bagUrl)
		break
	}

	s.UpdateLogStr()
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
			"https://www.apple.com/%s/%s?%s",
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

	for storeNumber, link := range reqs {
		go func(storeNumber, link string) {
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

	resp, body, errs := gorequest.New().
		Get(skUrl).
		Set("referer", "https://www.apple.com/shop/buy-iphone").
		Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.71 Safari/537.36").
		Timeout(time.Second * 10).End()

	if len(errs) > 0 {
		res.err = fmt.Errorf("网络错误: %v", errs[0])
		return res
	}

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

func (s *listenService) openBrowser(link string) {
	parse, err := url.Parse(link)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}

	err = view.App.OpenURL(parse)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
}

func (s *listenService) AlertMp3() {
	reader := bytes.NewReader(theme.Mp3().Content())
	streamer, _, err := mp3.Decode(ioutil.NopCloser(reader))
	if err != nil {
		panic(err)
	}
	defer streamer.Close()

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}

// barkClient 使用独立 client，默认 client 无超时，网络挂起时会永久占住 goroutine
var barkClient = &http.Client{Timeout: 10 * time.Second}

func (s *listenService) SendPushNotificationByBark(title string, content string, bagUrl string) {

	baseUrl := strings.TrimRight(strings.TrimSpace(s.GetBarkNotifyUrl()), "/")
	if baseUrl == "" {
		return
	}

	// 标题与内容会出现在 URL path 中，必须转义
	apiUrl := fmt.Sprintf(
		"%s/%s/%s?%s",
		baseUrl,
		url.PathEscape(title),
		url.PathEscape(content),
		url.Values{"url": []string{bagUrl}}.Encode(),
	)

	// 推送失败不应影响监听本身，记录日志即可
	response, err := barkClient.Get(apiUrl)
	if err != nil {
		log.Println("Bark 通知发送失败:", err)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		log.Println("Bark 通知返回异常状态:", response.Status)
	}
}
