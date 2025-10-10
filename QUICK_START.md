# 🚀 快速开始 - 发布第一个版本

## 立即发布版本（3 步完成）

### 1️⃣ 创建版本标签

在终端运行：

```bash
cd /Users/even/Downloads/apple-store-helper-master

# 创建版本标签（可以修改版本号）
git tag v1.0.1

# 推送标签到 GitHub
git push origin v1.0.1
```

### 2️⃣ 等待构建完成

- 打开浏览器访问：https://github.com/dashug/apple-store-helper/actions
- 等待 5-10 分钟，所有构建完成会显示绿色 ✅
- 如果失败显示红色 ❌，点击查看日志

### 3️⃣ 下载发布文件

- 访问：https://github.com/dashug/apple-store-helper/releases
- 找到 `v1.0.1` 版本
- 在 **Assets** 区域下载对应平台的文件

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

