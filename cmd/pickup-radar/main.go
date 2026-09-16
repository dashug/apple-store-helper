// pickup-radar 是取货雷达的命令行形态。
//
// 它不依赖图形环境，可以在服务器上长期挂着；命中有货时通过配置的
// 通知渠道（Bark / Server酱 / 企业微信 / Telegram / Webhook）提醒。
//
//	# 先看有哪些地区、门店与型号
//	pickup-radar --list-areas
//	pickup-radar --area 中国大陆 --list-stores
//	pickup-radar --area 中国大陆 --list-products
//
//	# 盯上海两家店的一个型号
//	pickup-radar --area 中国大陆 \
//	  --store 上海-环球港 --store 上海-南京东路 \
//	  --product "iphone18pro - 黑色 - 256gb" \
//	  --notify https://api.day.app/你的BarkKey
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"apple-store-helper/common"
	"apple-store-helper/model"
	"apple-store-helper/services"
)

// stringList 收集可重复出现的参数
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ", ") }

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("不能为空")
	}
	*l = append(*l, value)
	return nil
}

type options struct {
	area      string
	stores    stringList
	products  stringList
	notify    stringList
	interval  int
	once      bool
	keepGoing bool

	listAreas    bool
	listStores   bool
	listProducts bool
}

func main() {
	opts := parseFlags()

	if opts.listAreas {
		for _, area := range model.Areas {
			fmt.Println(area.Title)
		}
		return
	}

	area, err := resolveArea(opts.area)
	if err != nil {
		exitf("%v", err)
	}
	services.Listen.SetArea(area)

	if opts.listStores {
		printAll(services.Store.ByAreaTitleForOptions(area.Title))
		return
	}
	if opts.listProducts {
		printAll(services.Product.ByAreaTitleForOptions(area.Title))
		return
	}

	if err := configure(opts, area); err != nil {
		exitf("%v", err)
	}

	run(opts)
}

func parseFlags() options {
	var opts options

	flag.StringVar(&opts.area, "area", "", "地区，如「中国大陆」。用 --list-areas 查看全部")
	flag.Var(&opts.stores, "store", "门店名，可重复。用 --list-stores 查看全部")
	flag.Var(&opts.products, "product", "型号名，可重复。用 --list-products 查看全部")
	flag.Var(&opts.notify, "notify", "通知地址，可重复。支持 Bark / Server酱 / 企业微信 / Telegram / Webhook")
	flag.IntVar(&opts.interval, "interval", int(services.DefaultInterval/time.Second), "监听间隔秒数")
	flag.BoolVar(&opts.once, "once", false, "只查一轮就退出，便于配合 cron")
	flag.BoolVar(&opts.keepGoing, "keep-going", false, "命中后继续监听其余项，默认命中即暂停")

	flag.BoolVar(&opts.listAreas, "list-areas", false, "列出可选地区")
	flag.BoolVar(&opts.listStores, "list-stores", false, "列出该地区的门店")
	flag.BoolVar(&opts.listProducts, "list-products", false, "列出该地区的型号")

	version := flag.Bool("version", false, "显示版本")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "取货雷达（命令行）%s\n\n用法:\n", common.VERSION)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *version {
		fmt.Println(common.VERSION)
		os.Exit(0)
	}

	return opts
}

func resolveArea(title string) (model.Area, error) {
	if title == "" {
		// 不指定时用第一个地区，与图形版的默认一致
		return model.Areas[0], nil
	}

	for _, area := range model.Areas {
		if area.Title == title {
			return area, nil
		}
	}

	var names []string
	for _, area := range model.Areas {
		names = append(names, area.Title)
	}

	return model.Area{}, fmt.Errorf("未知地区 %q，可选：%s", title, strings.Join(names, "、"))
}

// configure 组装监听列表与通知渠道。
//
// 未指定门店或型号时回退到配置文件 —— 这样在图形版里配好之后，
// 把配置文件拷到服务器即可直接跑。
func configure(opts options, area model.Area) error {
	services.Listen.SetInterval(time.Duration(opts.interval) * time.Second)
	services.Listen.SetStopOnHit(!opts.keepGoing)
	services.Listen.SetNotifyUrls(strings.Join(opts.notify, "\n"))

	if len(opts.stores) > 0 || len(opts.products) > 0 {
		if len(opts.stores) == 0 || len(opts.products) == 0 {
			return fmt.Errorf("--store 与 --product 需要同时指定")
		}

		added, err := services.Listen.AddMany(area.Title, opts.stores, opts.products)
		if err != nil {
			return err
		}

		log.Printf("已添加 %d 项监听（%d 门店 × %d 型号）", added, len(opts.stores), len(opts.products))
		return nil
	}

	settings, err := services.LoadSettings()
	if err != nil {
		path, pathErr := services.SettingsPath()
		if pathErr != nil {
			path = "(无法确定路径)"
		}
		return fmt.Errorf("未指定 --store / --product，且读取配置失败\n  配置位置: %s\n  原因: %w", path, err)
	}

	services.Listen.SetListenItems(settings.ListenItems)
	if len(opts.notify) == 0 {
		services.Listen.SetBarkNotifyUrl(settings.BarkNotifyUrl)
		services.Listen.SetNotifyUrls(settings.NotifyUrls)
	}

	count := services.Listen.ActiveCount()
	if count == 0 {
		return fmt.Errorf("配置文件里没有属于「%s」的监听项", area.Title)
	}

	log.Printf("已从配置文件载入 %d 项监听", count)
	return nil
}

func run(opts options) {
	if len(services.Listen.NotifyTargets()) == 0 {
		log.Println("警告：未配置任何通知地址，命中有货时只会打印到日志")
	}

	services.Listen.SetOnInStock(func(event services.InStockEvent) {
		// 逐条列出：多家同时有货时，用户要据此决定去哪一家
		for _, item := range event.Items {
			log.Printf("★ 有货 %s %s", item.Store.CityStoreName, item.Product.Title)
		}
		log.Printf("  购物袋: %s", event.BagURL)
	})

	if opts.once {
		services.Listen.SetStatus(services.Running)
		services.Listen.RunOnce()
		printRows()
		return
	}

	log.Printf("开始监听，间隔 %v，按 Ctrl+C 退出", services.Listen.GetInterval())

	services.Listen.SetOnChange(printRows)
	services.Listen.Run()
	services.Listen.SetStatus(services.Running)

	// 等待退出信号，而不是空转
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	services.Listen.SetStatus(services.Pause)
	log.Println("已停止")
}

// printRows 打印当前各监听项的状态
func printRows() {
	for _, row := range services.Listen.SortedRows() {
		detail := ""
		if row.Detail != "" {
			detail = "  (" + row.Detail + ")"
		}
		log.Printf("[%s] %s %s%s", row.DisplayStatus(), row.Store.CityStoreName, row.Product.Title, detail)
	}
}

func printAll(values []string) {
	for _, v := range values {
		fmt.Println(v)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
