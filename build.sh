#!/bin/bash

# 取货雷达打包脚本
# 自动处理 macOS 应用签名问题

echo "🚀 开始打包取货雷达..."

# 清理之前的构建产物
if [ -d "fyne-cross" ]; then
    echo "🧹 清理旧的构建产物..."
    rm -rf fyne-cross/
fi

# 执行 fyne-cross 打包
echo "📦 执行打包..."
fyne-cross darwin -arch=amd64,arm64 -app-id=com.github.dashug.pickupradar -name="PickupRadar"

# 检查打包是否成功
if [ $? -ne 0 ]; then
    echo "❌ 打包失败"
    exit 1
fi

# 处理 ARM64 版本的签名问题
echo "🔐 处理 ARM64 版本签名..."
if [ -d "fyne-cross/dist/darwin-arm64/PickupRadar.app" ]; then
    # 清除扩展属性
    xattr -cr "fyne-cross/dist/darwin-arm64/PickupRadar.app"
    # 重新签名
    codesign --force --deep --sign - "fyne-cross/dist/darwin-arm64/PickupRadar.app"
    echo "✅ ARM64 版本签名完成"
fi

# 处理 AMD64 版本的签名（可选）
echo "🔐 处理 AMD64 版本签名..."
if [ -d "fyne-cross/dist/darwin-amd64/PickupRadar.app" ]; then
    # 清除扩展属性
    xattr -cr "fyne-cross/dist/darwin-amd64/PickupRadar.app"
    # 重新签名
    codesign --force --deep --sign - "fyne-cross/dist/darwin-amd64/PickupRadar.app"
    echo "✅ AMD64 版本签名完成"
fi

echo ""
echo "🎉 打包完成！"
echo "📍 应用位置："
echo "   - ARM64 (Apple Silicon): fyne-cross/dist/darwin-arm64/PickupRadar.app"
echo "   - AMD64 (Intel): fyne-cross/dist/darwin-amd64/PickupRadar.app"
echo ""
echo "💡 提示：直接双击即可运行应用"