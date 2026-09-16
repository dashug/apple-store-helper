package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"github.com/golang-module/carbon"

	"apple-store-helper/model"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
)

// TestGenerateScreenshot 重新生成 README 里的 screenshot.png。
//
// 用 fyne 测试画布的 Capture 软件渲染，不需要任何系统截屏权限，
// 而且渲染的就是 buildUI 组装的真实界面 —— 界面改了重跑一次即可，
// 不会再出现截图与实际界面脱节几年的情况。
//
//	GEN_SCREENSHOT=1 go test -run TestGenerateScreenshot .
func TestGenerateScreenshot(t *testing.T) {
	if os.Getenv("GEN_SCREENSHOT") == "" {
		t.Skip("设置 GEN_SCREENSHOT=1 重新生成 screenshot.png")
	}

	// isolateSettings 会把工作目录切到临时目录，
	// 因此先记下仓库路径，否则截图会写进临时目录后被丢弃
	repoDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// 不读写使用者真实的配置
	isolateSettings(t)

	app := fyne.CurrentApp()
	app.Settings().SetTheme(&theme.MyTheme{})

	window := app.NewWindow("Apple Store Helper")
	view.App = app
	view.Window = window

	// 2 倍渲染，否则 README 里的图在高分屏上发虚
	if scalable, ok := window.Canvas().(interface{ SetScale(float32) }); ok {
		scalable.SetScale(2)
	}

	// 先组装界面并填好数据，再 SetContent。
	//
	// 顺序很重要：SetContent 之后再改文字，控件仍按初始（空）文本的尺寸
	// 摆放，截图里会出现文字被裁切、重叠等实际运行中并不存在的现象 ——
	// 据此去「修」界面，只会把正确的代码改坏。
	content := buildUI().content
	fillSampleItems(t)

	window.SetContent(content)
	window.Resize(fyne.NewSize(1000, 800))

	img := window.Canvas().Capture()

	target := filepath.Join(repoDir, "screenshot.png")

	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("已生成 %s (%d×%d, %d KiB)",
		target, img.Bounds().Dx(), img.Bounds().Dy(), info.Size()/1024)
}

// fillSampleItems 通过服务层写入示例数据，从 goroutine 调用是有意的：
// 界面刷新回调内部走 fyne.Do，在非主 goroutine 中会同步执行，
// 这样截图时列表已经渲染完成。
func fillSampleItems(t *testing.T) {
	t.Helper()

	items := map[string]services.ListenItem{}
	add := func(store, product, status, detail string) {
		key := store + "." + product
		items[key] = services.ListenItem{
			Store:   model.Store{StoreNumber: store, CityStoreName: store},
			Product: model.Product{Code: product, Title: product},
			Status:  status,
			Detail:  detail,
			Time:    carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)},
		}
	}

	add("上海-环球港", "iphone18promax - 勃艮第酒红色 - 1tb", services.StatusInStock, "")
	add("上海-南京东路", "iphone18pro - 黑色 - 256gb", services.StatusUnknown, "接口返回 HTTP 541")
	add("北京-三里屯", "iphoneduo - 夜空色 - 512gb", services.StatusOutStock, "")
	add("广东-深圳益田假日广场", "iphoneair - 云白色 - 256gb", services.StatusOutStock, "")

	done := make(chan struct{})
	go func() {
		defer close(done)

		// 真实启动时 Run 会把状态置为「暂停」
		services.Listen.SetStatus(services.Pause)
		services.Listen.SetListenItems(items)
	}()
	<-done
}
