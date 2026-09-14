package main

import (
	"bytes"
	"io"
	"log"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"

	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
)

// handleInStock 是命中有货时的图形呈现。
//
// services 不再直接触碰 GUI（否则链接了 fyne 的二进制在无桌面环境起不来），
// 这些行为由界面层注册进去。
func handleInStock(event services.InStockEvent) {
	// 回调来自监听 goroutine，图形操作必须回到主运行时上下文
	fyne.Do(func() {
		// 窗口可能已收进托盘，命中时要让它重新出现，否则用户看不到提示
		view.Window.Show()
		view.Window.RequestFocus()

		openBrowser(event.BagURL)

		dialog.ShowInformation("匹配成功", event.Message, view.Window)
		view.App.SendNotification(&fyne.Notification{
			Title:   "有货提醒",
			Content: event.Message,
		})
	})

	go alertMp3()
}

// openBrowser 打开购物袋页面
func openBrowser(link string) {
	parsed, err := url.Parse(link)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}

	if err := view.App.OpenURL(parsed); err != nil {
		dialog.ShowError(err, view.Window)
	}
}

// alertMp3 播放有货提示音
func alertMp3() {
	streamer, _, err := mp3.Decode(io.NopCloser(bytes.NewReader(theme.Mp3().Content())))
	if err != nil {
		// 本函数总是以 go 调用，panic 会带崩整个程序
		log.Println("提示音解码失败:", err)
		return
	}
	defer streamer.Close()

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}
