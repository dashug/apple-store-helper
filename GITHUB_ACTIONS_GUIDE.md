# GitHub Actions 自动构建指南

## 🚀 快速开始

本项目已配置 GitHub Actions 自动构建工作流，可以自动编译生成多平台的可执行文件。

## 📋 支持的平台

✅ **Windows**
- Windows 64位 (amd64)
- Windows 32位 (386)

✅ **macOS**
- Apple Silicon (M1/M2/M3/M4) - arm64
- Intel 处理器 - amd64

✅ **Linux**
- Linux 64位 (amd64)

## 🎯 触发构建的方式

### 方式 1: 推送版本标签（推荐）

这是发布正式版本的标准方式：

```bash
# 1. 提交所有更改
git add .
git commit -m "准备发布新版本"

# 2. 创建版本标签（遵循语义化版本）
git tag v1.0.0

# 3. 推送代码和标签
git push origin main
git push origin v1.0.0
```

**标签命名规范**：
- `v1.0.0` - 正式版本
- `v1.0.1` - 修复版本
- `v1.1.0` - 新功能版本
- `v2.0.0` - 重大更新

推送标签后，GitHub Actions 会：
1. ✅ 自动构建所有平台的可执行文件
2. ✅ 创建 GitHub Release
3. ✅ 上传所有构建产物到 Release

### 方式 2: 手动触发

如果您只想测试构建，不想创建 Release：

1. 进入 GitHub 仓库页面
2. 点击 **Actions** 标签
3. 选择 **Build and Release** 工作流
4. 点击右侧的 **Run workflow** 按钮
5. 选择分支，点击 **Run workflow**

⚠️ **注意**：手动触发不会创建 Release，只会生成构建产物（Artifacts）

## 📦 下载构建产物

### 从 Release 下载（推荐）

1. 进入仓库的 **Releases** 页面
2. 选择最新的版本
3. 在 **Assets** 区域下载对应平台的文件：
   - `Apple-Store-Helper-macOS-arm64.zip` - macOS Apple Silicon
   - `Apple-Store-Helper-macOS-amd64.zip` - macOS Intel
   - `Apple-Store-Helper-Windows-amd64.zip` - Windows 64位
   - `Apple-Store-Helper-Windows-386.zip` - Windows 32位
   - `Apple-Store-Helper-Linux-amd64.tar.gz` - Linux 64位

### 从 Artifacts 下载（测试版本）

如果是手动触发的构建：

1. 进入 **Actions** 标签
2. 点击对应的工作流运行记录
3. 在页面底部的 **Artifacts** 区域下载
4. Artifacts 会在 90 天后自动删除

## 🔧 工作流配置文件

配置文件位置：`.github/workflows/build-release.yml`

### 主要步骤

```yaml
1. 检出代码
2. 安装 Go 1.22
3. 安装系统依赖（仅 Linux/macOS）
4. 安装 Fyne CLI
5. 编译应用程序
6. 处理 macOS 签名
7. 打包（zip/tar.gz）
8. 上传构建产物
9. 创建 Release（仅标签触发）
```

### 构建矩阵

工作流使用矩阵策略并行构建：

```yaml
- Windows: amd64, 386
- macOS: arm64, amd64
- Linux: amd64
```

## 🐛 常见问题

### Q1: 为什么我推送了标签但没有创建 Release？

**A**: 检查以下几点：
1. 标签格式是否正确（必须以 `v` 开头，如 `v1.0.0`）
2. 标签是否成功推送到 GitHub（`git push origin v1.0.0`）
3. 查看 Actions 页面是否有错误信息

### Q2: macOS 版本提示「无法打开」怎么办？

**A**: 这是 macOS 的安全机制。解决方法：
```bash
xattr -cr "/path/to/Apple Store Helper.app"
```

工作流会自动处理签名，但由于是自签名，首次打开可能仍需要在「系统偏好设置」中允许。

### Q3: 构建失败怎么办？

**A**: 
1. 查看 Actions 页面的详细日志
2. 常见失败原因：
   - Go 依赖下载失败 → 重新运行工作流
   - 系统依赖安装失败 → 检查 apt/brew 命令
   - 编译错误 → 检查代码是否有语法错误

### Q4: 如何修改构建配置？

**A**: 编辑 `.github/workflows/build-release.yml` 文件，例如：
- 修改 Go 版本：`go-version: '1.22'`
- 添加新平台：在 `matrix` 中添加新的架构
- 修改应用名称：修改 `-name` 参数

### Q5: 可以只构建特定平台吗？

**A**: 可以。在手动触发时：
1. 编辑工作流文件
2. 注释掉不需要的 job（在 job 名称前加 `#`）
3. 或者创建新的工作流文件只包含特定平台

## 📊 构建时间

预计构建时间（仅供参考）：
- Windows: ~3-5 分钟
- macOS: ~5-8 分钟
- Linux: ~3-5 分钟
- 总时间（并行）: ~8-10 分钟

## 🔐 权限要求

工作流需要以下权限：
- ✅ `contents: write` - 创建 Release 和上传文件
- ✅ `GITHUB_TOKEN` - GitHub 自动提供

## 📝 版本号管理建议

遵循 [语义化版本](https://semver.org/lang/zh-CN/) 规范：

- **主版本号 (Major)**: 不兼容的 API 修改
  - `v1.0.0` → `v2.0.0`
  
- **次版本号 (Minor)**: 向下兼容的功能性新增
  - `v1.0.0` → `v1.1.0`
  
- **修订号 (Patch)**: 向下兼容的问题修正
  - `v1.0.0` → `v1.0.1`

## 🎉 发布清单

每次发布新版本前：

- [ ] 测试所有功能是否正常
- [ ] 更新 README.md 中的版本信息
- [ ] 更新 CHANGELOG（如果有）
- [ ] 提交所有更改
- [ ] 创建并推送版本标签
- [ ] 等待 GitHub Actions 完成构建
- [ ] 检查 Release 页面的构建产物
- [ ] 下载并测试各平台的可执行文件
- [ ] 在 Release 描述中添加详细的更新说明

## 🔗 相关链接

- [GitHub Actions 文档](https://docs.github.com/cn/actions)
- [Fyne 文档](https://developer.fyne.io/)
- [Go 文档](https://go.dev/doc/)

---

**提示**：第一次使用 GitHub Actions 时，请确保在仓库设置中启用了 Actions 功能。

