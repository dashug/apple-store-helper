package services

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

	Pause   = "暂停"
	Running = "监听中"
)

var Listen = listenService{
	items:  map[string]ListenItem{},
	Status: binding.NewString(),
	Area:   model.Areas[0],
	Logs:   widget.NewLabel(""),
}

type listenService struct {
	items         map[string]ListenItem
	Status        binding.String
	Area          model.Area
	Logs          *widget.Label
	BarkNotifyUrl string
}

type ListenItem struct {
	Store   model.Store
	Product model.Product
	Status  string
	Time    carbon.DateTime
}

func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string, barkNotifyUrl string) {

	store := Store.GetStore(areaTitle, storeTitle)
	product := Product.GetProduct(areaTitle, productTitle)

	uniqKey := store.StoreNumber + "." + product.Code

	if s.items[uniqKey].Store.StoreNumber == "" {
		s.items[uniqKey] = ListenItem{
			Store:   store,
			Product: product,
			Status:  StatusWait,
		}
	}

	s.BarkNotifyUrl = barkNotifyUrl
	s.UpdateLogStr()
}

func (s *listenService) Clean() {
	s.items = map[string]ListenItem{}
	s.UpdateLogStr()
}

func (s *listenService) SetListenItems(items map[string]ListenItem) {
	s.items = items
	s.UpdateLogStr()
}

func (s *listenService) GetListenItems() map[string]ListenItem {
	return s.items
}

func (s *listenService) UpdateLogStr() {
	var str string

	for _, item := range s.items {

		str += fmt.Sprintf(
			"[%s] %s %s %s %s",
			item.Status,
			item.Time,
			item.Store.CityStoreName,
			item.Product.Title,
			"\n",
		)
	}

	s.Logs.SetText(str)
}

func (s *listenService) UpdateStatus(uniqKey string, status string) {
	item := s.items[uniqKey]
	item.Time = carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
	item.Status = status
	s.items[uniqKey] = item
}

func (s *listenService) Run() {
	s.Status.Set(Pause)

	go func() {
		for {
			if stats, ok := s.Status.Get(); ok == nil && stats == Running && len(s.items) > 0 {
				skus := s.groupByStore()

				for key, item := range s.items {
					status := skus[item.Store.StoreNumber+"."+item.Product.Code]

					if status {
						s.UpdateStatus(key, StatusInStock)
						s.Status.Set(Pause)

						// 中国大陆使用 .cn 域名，其他地区使用 .com 域名
						var bagUrl string
						if s.Area.ShortCode == "cn" {
							bagUrl = "https://www.apple.com.cn/shop/bag"
						} else {
							bagUrl = fmt.Sprintf("https://www.apple.com/%s/shop/bag", s.Area.ShortCode)
						}
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
					} else {
						s.UpdateStatus(key, StatusOutStock)
					}
				}

				s.UpdateLogStr()
			}

			time.Sleep(time.Millisecond * 500)
		}
	}()
}

func (s *listenService) groupByStore() map[string]bool {
	skus := map[string]bool{}

	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	group := map[string][]ListenItem{}
	reqs := map[string]string{}

	for _, item := range s.items {
		group[item.Store.StoreNumber] = append(group[item.Store.StoreNumber], item)
	}

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

		// Apple 已全面更新 API 端点，所有地区都使用 retail/pickup-message
		var baseURL string
		if s.Area.ShortCode == "cn" {
			// 中国大陆使用 .cn 域名
			baseURL = "https://www.apple.com.cn/shop/retail/pickup-message"
		} else {
			// 其他地区使用 .com 域名 + retail/pickup-message
			baseURL = fmt.Sprintf("https://www.apple.com/%s/shop/retail/pickup-message", s.Area.ShortCode)
		}

		link := fmt.Sprintf("%s?%s", baseURL, queryStr)
		log.Printf("请求 URL: %s", link)

		reqs[storeNumber] = link
	}

	count := len(reqs)
	if count < 1 {
		return skus
	}

	ch := make(chan map[string]bool, count)

	for _, link := range reqs {
		go s.getSkuByLink(ch, link)
	}

	for i := 0; i < count; i++ {
		for key, v := range <-ch {
			skus[key] = v
		}
	}

	return skus
}

func (s *listenService) getSkuByLink(ch chan map[string]bool, skUrl string) {
	skus := map[string]bool{}

	// 根据地区设置正确的 referer
	var referer string
	if s.Area.ShortCode == "cn" {
		referer = "https://www.apple.com.cn/shop/buy-iphone"
	} else {
		referer = fmt.Sprintf("https://www.apple.com/%s/shop/buy-iphone", s.Area.ShortCode)
	}

	resp, body, errs := gorequest.New().
		Get(skUrl).
		Set("referer", referer).
		Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.71 Safari/537.36").
		Timeout(time.Second * 3).End()
	if len(errs) > 0 {
		log.Println(errs)
		ch <- skus
		return
	}

	log.Println(resp.Status, skUrl)

	// 尝试两种可能的 JSON 路径（新旧 API 兼容）
	var stores gjson.Result
	newAPIPath := gjson.Get(body, "body.stores")
	oldAPIPath := gjson.Get(body, "body.content.pickupMessage.stores")

	if newAPIPath.Exists() && newAPIPath.IsArray() {
		stores = newAPIPath
		log.Println("使用新 API 路径: body.stores")
	} else if oldAPIPath.Exists() && oldAPIPath.IsArray() {
		stores = oldAPIPath
		log.Println("使用旧 API 路径: body.content.pickupMessage.stores")
	} else {
		log.Println("⚠️  未找到门店数据，API 响应可能有变化")
		log.Printf("响应体前 500 字符: %s", body[:min(500, len(body))])
		ch <- skus
		return
	}

	for _, result := range stores.Array() {
		storeNumber := result.Get("storeNumber").String()
		storeName := result.Get("storeName").String()
		log.Printf("检查门店: %s (%s)", storeName, storeNumber)

		for productCode, availability := range result.Get("partsAvailability").Map() {
			uniqKey := fmt.Sprintf("%s.%s", storeNumber, productCode)

			// 库存判断字段
			storeSelectionEnabled := availability.Get("messageTypes.compact.storeSelectionEnabled").Bool()
			pickupDisplay := availability.Get("pickupDisplay").String()

			log.Printf("  产品: %s", productCode)
			log.Printf("    storeSelectionEnabled: %v", storeSelectionEnabled)
			log.Printf("    pickupDisplay: %s", pickupDisplay)

			// 有货条件：storeSelectionEnabled 为 true 或 pickupDisplay 为 "available"
			isAvailable := storeSelectionEnabled || pickupDisplay == "available"

			if isAvailable {
				log.Printf("    ✅ 有货！")
			} else {
				log.Printf("    ❌ 无货")
			}

			skus[uniqKey] = isAvailable
		}
	}

	ch <- skus
}

// 型号对应预约地址
//func (s *listenService) model2Url(productType string) string {
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16-pro
//
//	var t string
//	switch productType {
//	case "iphone16promax", "iphone16pro":
//		t = "iphone-16-pro"
//	case "iphone16":
//		t = "iphone-16"
//	}
//
//	return fmt.Sprintf(
//		"https://www.apple.com/%s/shop/buy-iphone/%s",
//		s.Area.ShortCode,
//		t,
//	)
//}

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

func (s *listenService) SendPushNotificationByBark(title string, content string, bagUrl string) {

	if len(s.BarkNotifyUrl) <= 0 {
		return
	}

	apiUrl := fmt.Sprintf("%s/%s/%s?url=%s", strings.TrimRight(s.BarkNotifyUrl, "/"), title, content, bagUrl)

	response, err := http.Get(apiUrl)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
