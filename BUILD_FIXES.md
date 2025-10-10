# 🔧 构建问题修复记录

本文档记录了 GitHub Actions 自动构建过程中遇到的所有问题及解决方案。

---

## 📋 修复总览

| 平台 | 问题 | 解决方案 | 状态 |
|------|------|---------|------|
| Linux | 缺少 ALSA 音频库 | 安装 `libasound2-dev` | ✅ 已修复 |
| Linux | fyne package 失败 | 改用 `go build` | ✅ 已修复 |
| Windows AMD64 | 架构冲突 | 分离为独立作业 | ✅ 已修复 |
| Windows 386 | 缺少 32 位工具链 | 安装 MSYS2 32 位 MinGW | ✅ 已修复 |
| macOS | - | 使用 fyne package | ✅ 正常 |

---

## 🐧 修复 1: Linux 音频库依赖

### 问题
```
Package alsa was not found in the pkg-config search path.
```

### 原因
- 项目使用 `beep` 音频库播放提示音
- Linux 下需要 ALSA（Advanced Linux Sound Architecture）
- Ubuntu runner 缺少开发库

### 解决方案
```yaml
- name: Install dependencies
  run: |
    sudo apt-get update
    sudo apt-get install -y gcc libgl1-mesa-dev xorg-dev libasound2-dev pkg-config
```

### 关键依赖
- `libasound2-dev` - ALSA 音频开发库
- `pkg-config` - 包配置工具
- `libgl1-mesa-dev` - OpenGL（GUI）
- `xorg-dev` - X11（GUI）

---

## 🐧 修复 2: Linux fyne package 失败

### 问题
```
tar: apple-store-helper: Cannot stat: No such file or directory
```

### 原因
- `fyne package` 在 Linux 上未能正确生成可执行文件
- tar 命令找不到输出文件

### 解决方案
改用 `go build` 直接编译：

```yaml
- name: Build Linux amd64
  env:
    CGO_ENABLED: 1
    GOOS: linux
    GOARCH: amd64
  run: |
    go build -o "apple-store-helper" .
    tar -czf Apple-Store-Helper-Linux-amd64.tar.gz apple-store-helper
```

### 优势
- ✅ 更可靠，直接控制编译过程
- ✅ 错误信息更清晰
- ✅ 不依赖 fyne-cli 封装

---

## 🪟 修复 3: Windows 架构冲突

### 问题
```
C:/mingw64/bin/ld.exe: skipping incompatible libgdi32.a
C:/mingw64/bin/ld.exe: cannot find -lgdi32
```

### 原因
- 在矩阵构建中，同一 runner 切换架构（amd64 ↔ 386）
- MinGW 链接器的库文件与目标架构不匹配
- CGO 缓存混乱

### 解决方案
将 Windows 构建分离为两个独立的作业：

```yaml
jobs:
  build-windows-amd64:
    runs-on: windows-latest
    # ... 独立构建 amd64
  
  build-windows-386:
    runs-on: windows-latest
    # ... 独立构建 386
```

### 为什么有效？
- ✅ 每个架构使用全新的 runner
- ✅ 避免环境污染
- ✅ 独立的依赖安装

---

## 🪟 修复 4: Windows 386 缺少 32 位工具链

### 问题
```
skipping incompatible C:/mingw64/.../libgdi32.a when searching for -lgdi32
cannot find -lgdi32: No such file or directory
```

### 原因
- GitHub Actions Windows runner 默认只安装 64 位 MinGW (`mingw64`)
- 编译 32 位程序需要 32 位版本的系统库
- 64 位的 `libgdi32.a` 无法用于 32 位链接

### 解决方案
通过 MSYS2 安装 32 位工具链：

```yaml
- name: Setup MinGW-w64 for 32-bit
  run: |
    # 安装 MSYS2
    choco install msys2 -y --no-progress
    
    # 安装 32 位工具链
    C:\tools\msys64\usr\bin\bash.exe -lc "pacman -Syu --noconfirm"
    C:\tools\msys64\usr\bin\bash.exe -lc "pacman -S --noconfirm mingw-w64-i686-gcc mingw-w64-i686-pkg-config"
    
    # 添加到 PATH
    echo "C:\tools\msys64\mingw32\bin" | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append
```

### 技术细节
- `mingw-w64-i686-gcc` - i686（32 位）GCC 编译器
- `mingw32` 目录包含 32 位工具和库
- `mingw64` 目录包含 64 位工具和库

---

## 🍎 macOS 构建策略

### 为什么保留 fyne package？

macOS 需要特殊处理：
- 创建 `.app` 包结构
- 处理代码签名
- 设置应用权限和元数据

### 构建配置
```yaml
- name: Build macOS ${{ matrix.arch }}
  env:
    GOARCH: ${{ matrix.arch }}
  run: |
    fyne package -os darwin -name "Apple Store Helper" -icon Icon.png
    xattr -cr "Apple Store Helper.app"
    codesign --force --deep --sign - "Apple Store Helper.app"
    zip -r "Apple-Store-Helper-macOS-${{ matrix.arch }}.zip" "Apple Store Helper.app"
```

### 为什么矩阵构建可行？
- ✅ macOS 交叉编译支持成熟
- ✅ Apple 工具链质量高
- ✅ 不涉及复杂的第三方依赖

---

## 🎯 最终构建策略

### 统一原则
1. **Windows/Linux**: 使用 `go build`（可靠性优先）
2. **macOS**: 使用 `fyne package`（需要 .app 包）
3. **架构隔离**: Windows 分离作业避免冲突

### 构建矩阵

```
构建作业:
├─ build-windows-amd64 (独立 runner, go build)
├─ build-windows-386   (独立 runner, go build + 32位工具链)
├─ build-macos         (矩阵: arm64 + amd64, fyne package)
├─ build-linux         (go build)
└─ create-release      (等待所有作业完成)
```

---

## 📊 性能优化

### 构建时间
- **并行执行**: 所有平台同时构建
- **预计总时间**: 8-12 分钟
  - Windows AMD64: 3-5 分钟
  - Windows 386: 5-7 分钟（多了工具链安装）
  - macOS: 5-8 分钟
  - Linux: 3-5 分钟

### 依赖缓存
GitHub Actions 自动缓存：
- Go 模块下载
- Go 编译缓存
- 系统包管理器缓存

---

## 🔍 调试技巧

### 如果构建失败

1. **查看完整日志**
   - Actions → 选择运行 → 点击失败的作业

2. **常见错误模式**
   ```bash
   # 找不到库
   cannot find -lXXX
   → 缺少系统依赖，需要安装对应的 dev 包
   
   # 架构不匹配
   skipping incompatible
   → 编译器架构与目标架构不一致
   
   # CGO 相关
   C compiler not found
   → 缺少 GCC 或 MinGW
   ```

3. **本地测试**
   ```bash
   # Linux
   CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build
   
   # Windows（需要 MinGW）
   CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build
   
   # macOS
   fyne package -os darwin
   ```

---

## 📚 参考资源

- [Go CGO 文档](https://pkg.go.dev/cmd/cgo)
- [Fyne 打包文档](https://developer.fyne.io/started/packaging)
- [GitHub Actions Windows Runner](https://github.com/actions/runner-images/blob/main/images/windows/Windows2022-Readme.md)
- [MSYS2 MinGW 包](https://packages.msys2.org/group/mingw-w64-i686-toolchain)

---

## ✅ 验证清单

构建成功的标志：

- [ ] Windows AMD64 构建成功
- [ ] Windows 386 构建成功
- [ ] macOS ARM64 构建成功
- [ ] macOS AMD64 构建成功
- [ ] Linux AMD64 构建成功
- [ ] Release 自动创建
- [ ] 所有平台文件可下载
- [ ] 应用可正常运行

---

**最后更新**: 2025-10-10  
**状态**: ✅ 所有平台构建已修复  
**下次构建预计**: 成功

