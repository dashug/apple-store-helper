# Apple Store 预约助手

## 支持 iPhone 18 Pro / Pro Max、iPhone Duo、iPhone Air、iPhone 17 / 17e

![UI](screenshot.png)

## 重要提示
* *这不是外挂，不能全自动一劳永逸*
* *提前登录*
* *提前将需要购买的型号加入购物车，检测有货会打开购物车页面，需要在购物车页面手动选择门店*

## 关于开发
* 代码不优雅, 注释不完善, review须谨慎
* GUI框架 [fyne](https://github.com/fyne-io/fyne)

### 更新机型数据
新机型发布后，`config/files/products_*.json` 需要重新抓取：

```shell script
python3 scripts/fetch_products.py             # 全部地区
python3 scripts/fetch_products.py --dry-run   # 只看结果，不写文件
```

数据源是各地区购买页内嵌的 `productSelectionData`。若购买页地址有变动，
修改脚本顶部的 `SLUGS` 即可。

检查是否出现了尚未收录的新机型：

```shell script
python3 scripts/fetch_products.py --check-new
```

它会扫描各地区的 `/shop/buy-iphone` 索引页，并逐个验证候选页面确实是有效
购买页（索引页会残留已下架机型的链接）。确认要监控的加进 `SLUGS`，确认不
需要的加进 `IGNORED_SLUGS`。

以上两件事每天由 [`update-products.yml`](.github/workflows/update-products.yml)
自动执行：数据有变化或发现未收录机型时会自动提交 PR，等待人工确认后合并。

### 运行
```shell script
go run main.go
```

### 打包

#### 快速打包（推荐）
```shell script
# 使用打包脚本（自动处理签名问题）
./build.sh
```

#### 手动打包
```shell script
# Mac OS 环境下打包
# fyne CLI 已迁移，旧路径 fyne.io/fyne/v2/cmd/fyne 官方标记为 deprecated
go install fyne.io/tools/cmd/fyne@latest
go install github.com/fyne-io/fyne-cross@latest

# 基础打包命令
fyne-cross darwin -arch=amd64,arm64 -app-id=apple.store.helper -name="Apple Store Helper"
fyne-cross windows -arch=amd64,386 -app-id=apple.store.helper -name="Apple Store Helper"

# macOS ARM64 版本需要额外处理签名
xattr -cr "fyne-cross/dist/darwin-arm64/Apple Store Helper.app"
codesign --force --deep --sign - "fyne-cross/dist/darwin-arm64/Apple Store Helper.app"
```

如果提示 `fyne-cross: command not found`，请配置 GO 环境变量  
添加以下内容到 `~/.zshrc` 或 `~/.bashrc` 中
```shell script
# GOLANG
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
```
GOROOT 为 GO 安装目录，根据实际安装位置修改

## 使用方法

1. 前往 [release](https://github.com/dashug/apple-store-helper/releases) 页面下载对应系统的程序，启动 
2. 在 Apple 官网将需要购买的型号加入购物车
3. 选择地区、门店和型号，点击`添加`按钮，将需要监听的型号添加到监听列表
4. 点击`开始`按钮开始监听，检测到有货时会自动打开购物车页面
5. 匹配到有货后会自动暂停监听，直到再次点击 `开始`

### 关于监听间隔
默认 5 秒。间隔越短越早发现有货，但请求越密也越容易被 Apple 限流 ——
被限流时所有型号会显示`未知`，恰好在最需要结果的时刻拿不到结果。
连续查询失败时程序会自动退避，恢复正常后立即回到设定的间隔。

### 有货时推送通知到 iOS 设备
1. App Store 下载并安装 App 「Bark」，并允许「Bark」进行推送
2. 打开「Bark」，复制应用中代表你自己设备的地址（格式如` https://api.day.app/xxxxxxxxx `），粘贴至本应用的`Bark 通知地址`栏
3. 点击 `测试 Bark 通知`，确认应用能够通知到你的 iOS 设备
4. 更多内容请参考 `https://bark.day.app/`

## Contributors
- [@Hteen](https://github.com/hteen)
- [@Timssse](https://github.com/Timssse)
- [@Black-Hole](https://github.com/BlackHole1)
- [@RayJason](https://github.com/RayJason)
- [@Warkeeper](https://github.com/Warkeeper)

