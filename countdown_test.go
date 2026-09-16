package main

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-module/carbon"

	"apple-store-helper/services"
)

// 退避把间隔拉到几分钟时，状态栏必须说明还要等多久，
// 否则用户看到的就是一个半天不动的时间戳，分不清是退避还是卡死
func TestFormatCountdown(t *testing.T) {
	cases := []struct {
		left time.Duration
		want string
	}{
		{5 * time.Second, "5 秒"},
		{59 * time.Second, "59 秒"},
		{90 * time.Second, "1 分 30 秒"},
		{5 * time.Minute, "5 分 00 秒"},
	}

	for _, c := range cases {
		if got := formatCountdown(c.left); got != c.want {
			t.Errorf("%v 应显示为 %q，实际 %q", c.left, c.want, got)
		}
	}
}

// 已经到点或尚未安排时不显示倒计时，避免「下一轮 0 秒」长期挂着
func TestNoCountdownWhenDue(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if got := formatCountdown(d); got != "" {
			t.Errorf("%v 不应显示倒计时，实际 %q", d, got)
		}
	}
}

func TestStatusTextShowsCountdownOnlyWhileRunning(t *testing.T) {
	next := time.Now().Add(30 * time.Second)
	last := carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}

	running := statusText(services.Running, 3, 0, last, next)
	if !strings.Contains(running, "下一轮") {
		t.Errorf("监听中应显示倒计时，实际 %q", running)
	}

	paused := statusText(services.Pause, 3, 0, last, next)
	if strings.Contains(paused, "下一轮") {
		t.Errorf("暂停时没有下一轮，不该显示倒计时，实际 %q", paused)
	}
}

// 项数、停用数、上轮时间这些原有信息不能因为加倒计时而丢失
func TestStatusTextKeepsExistingInfo(t *testing.T) {
	last := carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
	got := statusText(services.Running, 5, 2, last, time.Time{})

	for _, want := range []string{services.Running, "5 项", "2 已停用", last.ToTimeString()} {
		if !strings.Contains(got, want) {
			t.Errorf("状态栏应包含 %q，实际 %q", want, got)
		}
	}
	if strings.Contains(got, "下一轮") {
		t.Errorf("尚未安排下一轮时不应显示倒计时，实际 %q", got)
	}
}
