package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDetectChannel(t *testing.T) {
	cases := map[string]string{
		"https://api.day.app/abcdefg":                               ChannelBark,
		"https://sctapi.ftqq.com/SCTxxxx.send":                      ChannelServerChan,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x":    ChannelWeCom,
		"https://api.telegram.org/bot123:abc/sendMessage?chat_id=1": ChannelTelegram,
		"https://example.com/my-hook":                               ChannelWebhook,
		"不是一个地址":                                                    ChannelWebhook,
	}

	for target, want := range cases {
		if got := DetectChannel(target); got != want {
			t.Errorf("%s\n期望: %s\n实际: %s", target, want, got)
		}
	}
}

// 自建 Bark 的域名不是 day.app，必须能通过显式渠道覆盖识别结果，
// 否则会被当成通用 Webhook 发出去，用户收不到任何通知
func TestExplicitChannelOverridesDetection(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	defer srv.Close()

	// 不指定渠道：按地址识别成 Webhook
	if got := resolveChannel(NotifyTarget{URL: srv.URL}); got != ChannelWebhook {
		t.Errorf("自建地址默认应识别为 Webhook，实际 %s", got)
	}

	// 显式指定为 Bark：走 Bark 的 path 形式
	res := sendOne(NotifyTarget{URL: srv.URL + "/mykey", Channel: ChannelBark},
		Notification{Title: "有货提醒", Content: "测试", URL: "https://example.com/bag"})
	if res.Err != nil {
		t.Fatalf("发送失败: %v", res.Err)
	}
	if res.Channel != ChannelBark {
		t.Errorf("渠道应为 Bark，实际 %s", res.Channel)
	}
	if !strings.HasPrefix(gotPath, "/mykey/有货提醒/") {
		t.Errorf("未走 Bark 的 path 形式，实际 %q", gotPath)
	}
}

func TestBarkRequestShape(t *testing.T) {
	var method, path, bagUrl string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, bagUrl = r.Method, r.URL.Path, r.URL.Query().Get("url")
	}))
	defer srv.Close()

	sendOne(NotifyTarget{URL: srv.URL + "/key", Channel: ChannelBark},
		Notification{Title: "有货提醒", Content: "含?问号#井号", URL: "https://example.com/bag"})

	if method != http.MethodGet {
		t.Errorf("Bark 应为 GET，实际 %s", method)
	}
	if path != "/key/有货提醒/含?问号#井号" {
		t.Errorf("标题正文未正确转义进 path，实际 %q", path)
	}
	if bagUrl != "https://example.com/bag" {
		t.Errorf("url 参数被破坏: %q", bagUrl)
	}
}

func TestServerChanRequestShape(t *testing.T) {
	var method, ctype string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ctype = r.Method, r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
	}))
	defer srv.Close()

	sendOne(NotifyTarget{URL: srv.URL + "/SCTxxx.send", Channel: ChannelServerChan},
		Notification{Title: "有货提醒", Content: "环球港有货", URL: "https://example.com/bag"})

	if method != http.MethodPost {
		t.Errorf("Server酱 应为 POST，实际 %s", method)
	}
	if !strings.Contains(ctype, "x-www-form-urlencoded") {
		t.Errorf("Content-Type 不对: %q", ctype)
	}
	if form.Get("title") != "有货提醒" {
		t.Errorf("title 不对: %q", form.Get("title"))
	}
	if !strings.Contains(form.Get("desp"), "环球港有货") || !strings.Contains(form.Get("desp"), "example.com/bag") {
		t.Errorf("desp 应含正文与链接: %q", form.Get("desp"))
	}
}

func TestWeComRequestShape(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}))
	defer srv.Close()

	sendOne(NotifyTarget{URL: srv.URL, Channel: ChannelWeCom},
		Notification{Title: "有货提醒", Content: "环球港有货", URL: "https://example.com/bag"})

	if payload["msgtype"] != "text" {
		t.Errorf("msgtype 应为 text，实际 %v", payload["msgtype"])
	}
	text, _ := payload["text"].(map[string]any)
	content, _ := text["content"].(string)
	for _, want := range []string{"有货提醒", "环球港有货", "example.com/bag"} {
		if !strings.Contains(content, want) {
			t.Errorf("content 缺少 %q: %q", want, content)
		}
	}
}

// chat_id 由用户配在地址里，附加 text 时不能把它冲掉
func TestTelegramKeepsChatID(t *testing.T) {
	var chatID, text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatID, text = r.URL.Query().Get("chat_id"), r.URL.Query().Get("text")
	}))
	defer srv.Close()

	sendOne(NotifyTarget{URL: srv.URL + "/bot123/sendMessage?chat_id=98765", Channel: ChannelTelegram},
		Notification{Title: "有货提醒", Content: "环球港有货", URL: "https://example.com/bag"})

	if chatID != "98765" {
		t.Errorf("chat_id 丢失或被覆盖: %q", chatID)
	}
	if !strings.Contains(text, "环球港有货") {
		t.Errorf("text 不对: %q", text)
	}
}

func TestWebhookRequestShape(t *testing.T) {
	var payload map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}))
	defer srv.Close()

	sendOne(NotifyTarget{URL: srv.URL},
		Notification{Title: "有货提醒", Content: "环球港有货", URL: "https://example.com/bag"})

	if payload["title"] != "有货提醒" || payload["content"] != "环球港有货" || payload["url"] != "https://example.com/bag" {
		t.Errorf("Webhook JSON 字段不对: %+v", payload)
	}
}

// 非 200 必须报错，否则配错地址会一直静默失败
func TestNon200IsReportedAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	res := sendOne(NotifyTarget{URL: srv.URL}, Notification{Title: "t", Content: "c"})
	if res.Err == nil {
		t.Fatal("非 200 应返回错误")
	}
	if !strings.Contains(res.Err.Error(), "403") || !strings.Contains(res.Err.Error(), "invalid webhook") {
		t.Errorf("错误信息应包含状态码与对端返回，实际: %v", res.Err)
	}
}

func TestSplitTargetsIgnoresBlankAndComments(t *testing.T) {
	raw := "  https://a.example/1  \n\n# 这是注释\nhttps://b.example/2\n\t\n"

	got := splitTargets(raw)
	if len(got) != 2 {
		t.Fatalf("应得到 2 个目标，实际 %d: %+v", len(got), got)
	}
	if got[0].URL != "https://a.example/1" || got[1].URL != "https://b.example/2" {
		t.Errorf("拆分结果不对: %+v", got)
	}
}

// 配了多个渠道时要逐条返回结果，否则坏了一个用户不知道是哪个
func TestNotifyReportsEachTarget(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()

	svc := newListenService()
	svc.SetBarkNotifyUrl(ok.URL + "/barkkey")
	svc.SetNotifyUrls(ok.URL + "/hook\nhttp://127.0.0.1:1/dead")

	results := svc.Notify(Notification{Title: "有货提醒", Content: "测试"})

	if len(results) != 3 {
		t.Fatalf("应有 3 条结果，实际 %d", len(results))
	}
	if results[0].Channel != ChannelBark || results[0].Err != nil {
		t.Errorf("第一条应为成功的 Bark，实际 %+v", results[0])
	}
	if results[1].Err != nil {
		t.Errorf("第二条应成功，实际 %v", results[1].Err)
	}
	if results[2].Err == nil {
		t.Error("第三条不可达，应返回错误")
	}
}

func TestNotifyWithNoTargetsSendsNothing(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	svc := newListenService()
	svc.SetBarkNotifyUrl("   ")
	svc.SetNotifyUrls("\n  \n# 只有注释\n")

	if results := svc.Notify(Notification{Title: "t"}); len(results) != 0 {
		t.Errorf("未配置时不应产生结果，实际 %+v", results)
	}
	if hits != 0 {
		t.Errorf("未配置时不应发请求，实际 %d 次", hits)
	}
}
