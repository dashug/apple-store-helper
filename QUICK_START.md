# 🚀 快速开始 - 自动构建和发布

## ✨ 自动构建已启用！

现在**每次推送代码到 main 分支**都会：
1. ✅ 自动构建所有平台的可执行文件
2. ✅ 自动创建 Release（带日期的预发布版本）
3. ✅ 自动上传所有构建产物

## 🎯 两种发布方式

### 方式 1: 自动构建版本（推荐日常使用）

**只需推送代码即可**：

```bash
# 修改代码后
git add .
git commit -m "你的提交信息"
git push origin main
```

然后自动触发构建，生成版本号如：`v2025.10.10-build.a29c0c5`

### 方式 2: 正式发布版本（重大更新）

**创建版本标签**：

```bash
# 创建正式版本标签
git tag v1.0.1

# 推送标签
git push origin v1.0.1
```

生成版本号：`v1.0.1`（正式版本）

---

## 📊 版本区别

| 类型 | 触发方式 | 版本号示例 | 标记 | 用途 |
|------|---------|-----------|------|------|
| 🚀 自动构建 | 推送代码 | `v2025.10.10-build.a29c0c5` | Pre-release | 日常测试 |
| ⭐ 正式版本 | 推送标签 | `v1.0.1` | Release | 稳定发布 |

---

## 🔍 查看构建状态

- **Actions**: https://github.com/dashug/apple-store-helper/actions
- **Releases**: https://github.com/dashug/apple-store-helper/releases

---

## 💡 现在就试试！

立即推送代码来触发第一次自动构建：

```bash
cd /Users/even/Downloads/apple-store-helper-master
git push origin main
```

等待 5-10 分钟后访问 Releases 页面即可下载！

---

## 📦 发布的文件

自动生成以下文件：

```
✅ Apple-Store-Helper-macOS-arm64.zip      (macOS Apple Silicon)
✅ Apple-Store-Helper-macOS-amd64.zip      (macOS Intel)
✅ Apple-Store-Helper-Windows-amd64.zip    (Windows 64位)
✅ Apple-Store-Helper-Windows-386.zip      (Windows 32位)
✅ Apple-Store-Helper-Linux-amd64.tar.gz   (Linux 64位)
```

---

## 🎯 推荐的版本号

根据您的更改类型选择：

### 第一个正式版本
```bash
git tag v1.0.0
```

### API 修复版本（当前情况）
```bash
git tag v1.0.1  # 推荐使用这个
```

### 新功能版本
```bash
git tag v1.1.0
```

---

## ⚡ 或者手动触发构建（不创建 Release）

如果您只想测试构建：

1. 访问：https://github.com/dashug/apple-store-helper/actions
2. 点击 **Build and Release**
3. 点击右侧的 **Run workflow**
4. 选择 `main` 分支
5. 点击绿色的 **Run workflow** 按钮

构建完成后，在工作流页面底部的 **Artifacts** 下载测试文件。

---

## 🔍 查看构建状态

实时查看构建进度：
https://github.com/dashug/apple-store-helper/actions

---

## ❓ 遇到问题？

查看详细指南：`GITHUB_ACTIONS_GUIDE.md`

