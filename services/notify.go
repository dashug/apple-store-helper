package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 通知渠道。Bark 只覆盖 iOS，其余渠道让 Android、桌面与群聊用户也能收到提醒。
const (
	ChannelBark       = "Bark"
	ChannelServerChan = "Server酱"
	ChannelWeCom      = "企业微信"
	ChannelTelegram   = "Telegram"
	ChannelWebhook    = "Webhook"
)

// notifyClient 与库存查询共用超时策略，但独立成一个 client：
// 推送慢不应该影响监听本身
var notifyClient = &http.Client{Timeout: 10 * time.Second}

// Notification 是一条待发送的提醒
type Notification struct {
	Title   string
	Content string
	URL     string
}

// NotifyTarget 是一个通知目标。
//
// Channel 为空表示按地址自动识别；Bark 字段会显式指定为 ChannelBark ——
// Bark 支持自建，自建地址的域名不是 day.app，靠识别会被误判成普通 Webhook。
type NotifyTarget struct {
	URL     string
	Channel string
}

// NotifyResult 是单个目标的发送结果。
// 逐条返回而不是只报一个总错误，否则配了三个渠道、坏了一个，用户无从判断是哪个。
type NotifyResult struct {
	Target  string
	Channel string
	Err     error
}

// DetectChannel 按地址判断渠道。无法识别的一律按通用 Webhook 处理。
func DetectChannel(target string) string {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return ChannelWebhook
	}

	switch host := strings.ToLower(parsed.Host); {
	case strings.HasSuffix(host, "day.app"):
		return ChannelBark
	case strings.HasSuffix(host, "ftqq.com"):
		return ChannelServerChan
	case strings.HasSuffix(host, "qyapi.weixin.qq.com"):
		return ChannelWeCom
	case strings.HasSuffix(host, "api.telegram.org"):
		return ChannelTelegram
	default:
		return ChannelWebhook
	}
}

// sendBark 走 {地址}/{标题}/{正文}?url=... 形式，标题与正文在 path 中必须转义
func sendBark(target string, n Notification) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(target), "/")

	link := fmt.Sprintf("%s/%s/%s?%s",
		base,
		url.PathEscape(n.Title),
		url.PathEscape(n.Content),
		url.Values{"url": []string{n.URL}}.Encode(),
	)

	return http.NewRequest(http.MethodGet, link, nil)
}

// sendServerChan 走表单 title + desp
func sendServerChan(target string, n Notification) (*http.Request, error) {
	form := url.Values{
		"title": []string{n.Title},
		"desp":  []string{n.Content + "\n\n" + n.URL},
	}

	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req, nil
}

// sendWeCom 走群机器人的文本消息
func sendWeCom(target string, n Notification) (*http.Request, error) {
	payload := map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": n.Title + "\n" + n.Content + "\n" + n.URL},
	}

	return jsonRequest(target, payload)
}

// sendTelegram 把文本作为查询参数附加，chat_id 由用户配在地址里
func sendTelegram(target string, n Notification) (*http.Request, error) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return nil, err
	}

	query := parsed.Query()
	query.Set("text", n.Title+"\n"+n.Content+"\n"+n.URL)
	parsed.RawQuery = query.Encode()

	return http.NewRequest(http.MethodGet, parsed.String(), nil)
}

// sendWebhook 是兜底形式，POST 一个结构化 JSON，由对端自行解析
func sendWebhook(target string, n Notification) (*http.Request, error) {
	return jsonRequest(target, map[string]string{
		"title":   n.Title,
		"content": n.Content,
		"url":     n.URL,
	})
}

func jsonRequest(target string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

// resolveChannel 返回目标实际使用的渠道
func resolveChannel(target NotifyTarget) string {
	if target.Channel != "" {
		return target.Channel
	}
	return DetectChannel(target.URL)
}

// buildNotifyRequest 按渠道组装请求，单独拆出来便于测试请求形状
func buildNotifyRequest(target NotifyTarget, n Notification) (*http.Request, string, error) {
	channel := resolveChannel(target)

	var (
		req *http.Request
		err error
	)
	switch channel {
	case ChannelBark:
		req, err = sendBark(target.URL, n)
	case ChannelServerChan:
		req, err = sendServerChan(target.URL, n)
	case ChannelWeCom:
		req, err = sendWeCom(target.URL, n)
	case ChannelTelegram:
		req, err = sendTelegram(target.URL, n)
	default:
		req, err = sendWebhook(target.URL, n)
	}

	return req, channel, err
}

// sendOne 向单个目标发送
func sendOne(target NotifyTarget, n Notification) NotifyResult {
	result := NotifyResult{Target: target.URL, Channel: resolveChannel(target)}

	req, _, err := buildNotifyRequest(target, n)
	if err != nil {
		result.Err = fmt.Errorf("地址无法解析: %w", err)
		return result
	}

	resp, err := notifyClient.Do(req)
	if err != nil {
		result.Err = err
		return result
	}
	defer resp.Body.Close()

	// 读掉响应体，连接才能复用
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		result.Err = fmt.Errorf("HTTP %d %s", resp.StatusCode, snippet)
	}

	return result
}

// splitTargets 按行拆分通知地址，忽略空行与 # 开头的注释行。
// 这些地址按内容自动识别渠道。
func splitTargets(raw string) []NotifyTarget {
	var targets []NotifyTarget

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, NotifyTarget{URL: line})
	}

	return targets
}
