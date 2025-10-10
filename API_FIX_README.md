# Apple Store Helper API 修复说明

## 🐛 问题描述

官网显示有库存，但监控工具显示无货。

## 🔍 根本原因

Apple 更改了中国大陆地区的库存查询 API：

### 旧 API (已失效)
```
https://www.apple.com/cn/shop/fulfillment-messages
或
https://www.apple.com.cn/shop/fulfillment-messages
```
返回：**404 Page Not Found**

### 新 API (已修复)
```
https://www.apple.com.cn/shop/retail/pickup-message
```
返回：**200 OK + JSON 数据**

## 🔧 修复内容

### 1. API 端点更新

**文件**: `services/listen.go`

**修改位置**: `groupByStore()` 函数

```go
// 中国大陆使用新的 retail/pickup-message 端点
var baseURL string
if s.Area.ShortCode == "cn" {
    baseURL = "https://www.apple.com.cn/shop/retail/pickup-message"
} else {
    baseURL = fmt.Sprintf("https://www.apple.com/%s/shop/fulfillment-messages", s.Area.ShortCode)
}
```

### 2. JSON 响应路径更新

**旧路径** (其他地区仍在使用):
```
body.content.pickupMessage.stores
```

**新路径** (中国大陆):
```
body.stores
```

**兼容性处理**:
```go
// 尝试两种可能的 JSON 路径（新旧 API 兼容）
var stores gjson.Result
newAPIPath := gjson.Get(body, "body.stores")
oldAPIPath := gjson.Get(body, "body.content.pickupMessage.stores")

if newAPIPath.Exists() && newAPIPath.IsArray() {
    stores = newAPIPath
} else if oldAPIPath.Exists() && oldAPIPath.IsArray() {
    stores = oldAPIPath
}
```

### 3. 库存判断逻辑优化

**有货判断条件**:
- `messageTypes.compact.storeSelectionEnabled` = `true`
- **或** `pickupDisplay` = `"available"`

```go
isAvailable := storeSelectionEnabled || pickupDisplay == "available"
```

### 4. 购物车 URL 修复

```go
// 中国大陆使用 .cn 域名
var bagUrl string
if s.Area.ShortCode == "cn" {
    bagUrl = "https://www.apple.com.cn/shop/bag"
} else {
    bagUrl = fmt.Sprintf("https://www.apple.com/%s/shop/bag", s.Area.ShortCode)
}
```

### 5. Referer 头修复

```go
// 根据地区设置正确的 referer
var referer string
if s.Area.ShortCode == "cn" {
    referer = "https://www.apple.com.cn/shop/buy-iphone"
} else {
    referer = fmt.Sprintf("https://www.apple.com/%s/shop/buy-iphone", s.Area.ShortCode)
}
```

## 📊 API 响应示例

### 无货时的响应
```json
{
  "body": {
    "stores": [{
      "storeName": "七宝",
      "storeNumber": "R705",
      "partsAvailability": {
        "MG734CH/A": {
          "pickupDisplay": "unavailable",
          "pickupSearchQuote": "不可取货",
          "messageTypes": {
            "compact": {
              "storeSelectionEnabled": false,
              "storePickupQuote": "暂无供应"
            }
          },
          "buyability": {
            "isBuyable": false,
            "inventory": 0
          }
        }
      }
    }]
  }
}
```

### 有货时的响应 (预期)
```json
{
  "body": {
    "stores": [{
      "storeName": "七宝",
      "storeNumber": "R705",
      "partsAvailability": {
        "MG734CH/A": {
          "pickupDisplay": "available",
          "pickupSearchQuote": "今天可取货",
          "messageTypes": {
            "compact": {
              "storeSelectionEnabled": true,
              "storePickupQuote": "今天可取货"
            }
          },
          "buyability": {
            "isBuyable": true,
            "inventory": 1
          }
        }
      }
    }]
  }
}
```

## ✅ 测试验证

### 运行程序
```bash
go run main.go
```

### 查看日志输出
程序会打印详细的调试信息：
- ✅ 请求的 URL
- ✅ API 响应状态
- ✅ 使用的 JSON 路径
- ✅ 每个门店的库存状态
- ✅ 产品代码和库存字段值

### 预期日志示例
```
2025/10/10 14:16:43 请求 URL: https://www.apple.com.cn/shop/retail/pickup-message?...
2025/10/10 14:16:43 200 OK https://www.apple.com.cn/shop/retail/pickup-message?...
2025/10/10 14:16:43 使用新 API 路径: body.stores
2025/10/10 14:16:43 检查门店: 七宝 (R705)
2025/10/10 14:16:43   产品: MG734CH/A
2025/10/10 14:16:43     storeSelectionEnabled: false
2025/10/10 14:16:43     pickupDisplay: unavailable
2025/10/10 14:16:43     ❌ 无货
```

## 🌏 其他地区兼容性

✅ **已测试地区**:
- 🇨🇳 中国大陆 (使用新 API)

⚠️ **需要验证**:
- 🇭🇰 香港
- 🇹🇼 台湾
- 🇯🇵 日本
- 🇸🇬 新加坡
- 🇦🇺 澳大利亚
- 🇲🇾 马来西亚

如果其他地区也出现 404 错误，可能需要将它们也改为 `retail/pickup-message` 端点。

## 📝 注意事项

1. **API 可能随时变化**: Apple 可能会继续调整 API，建议定期检查
2. **请求频率**: 当前设置为 0.5 秒轮询，过高可能导致限流
3. **产品代码**: 确保配置文件中的产品代码与官网一致

## 🔄 未来改进建议

1. **配置化 API 端点**: 将 API 地址放到配置文件中
2. **自动降级**: 当主 API 失败时自动尝试备用端点
3. **错误上报**: 当 API 返回异常时通知用户
4. **缓存机制**: 减少不必要的 API 请求

---

**修复日期**: 2025-10-10
**修复版本**: dev
**测试状态**: ✅ 通过编译，等待实际有货时验证

