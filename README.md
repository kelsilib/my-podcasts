<div align="center">
  <img src="resources/icon.png" alt="My Podcasts Icon" width="108" height="108" />
  <h1>My Podcasts</h1>
  <p><strong>轻量、原生、高颜值的 Windows 端 Apple Podcasts 桌面封装客户端</strong></p>
  <p>基于纯 Go + <a href="https://github.com/egoist/mygo">MyGo</a> 原生 GPU 渲染打造 · 告别 Electron 内存占用 · 零外部解码器依赖</p>
</div>

---

## ✨ 这是什么？

**My Podcasts** 是一款专为 Windows 打造的 **Apple Podcasts 非官方原生桌面客户端（封装软件）**。

Apple 官方长期未提供独立的 Windows 版 Apple Podcasts 桌面应用，网页版又缺少良好的桌面播放体验、快捷键联动与本地记忆能力。My Podcasts 直接对接 Apple Podcasts 官方 Web API（`amp-api.podcasts.apple.com`），以原生 Direct3D 11 渲染界面与纯 Go 流媒体音频引擎，为你提供接近 macOS 原生客户端的收听体验。

---

## 🚀 核心特色功能

- **🌍 一键换区浏览（US / CN / JP / GB）**
  - 顶部标题栏一键切换商店地区（美区、国区、日区、英区），无需修改系统区域或退出账号，即可畅听不同地区的热门节目、最新单集与搜索结果。
  - 登录后自动识别 Apple ID 归属地区，跨区浏览时仍可准确补拉云端播放进度。
- **☁️ Apple ID 登录与全页面跨设备进度同步**
  - 登录时按需拉起临时无痕 **WebView2** 窗口完成认证，获取会话后立刻销毁浏览器进程，**无后台浏览器常驻内存**。
  - 登录凭据使用 Windows **DPAPI**（当前用户密钥）本地加密存储，不留明文密码或 Token，并支持多账号会话彻底隔离。
  - 同步 **我的订阅、已存单集、云端继续收听、订阅最新更新、最近收听历史**；支持跨全页面（发现页、待播与更新、已存单集、节目详情、单集详情、搜索结果、沉浸视图）实时显示精确进度条与「✓ 已播完」状态，播放/暂停/跳转/播完均自动回写 iCloud。
- **🎵 macOS 风格沉浸视图与逐字文稿**
  - 参考 macOS Apple Music 歌词页设计：动态提取封面主色生成丝绒渐变与呼吸光晕，进入/退出自带弹簧动效与共享元素封面飞行过渡，进入沉浸视图自动隐藏底部播放栏。
  - 支持读取 RSS Podcasting 2.0 `<podcast:transcript>` 逐字文稿（JSON / WebVTT / SRT），随播放高亮滚动且点击任意一句即可跳转；同时自动解析单集简介中的章节时间轴；无文稿时优雅回退为居中大封面纯净模式。
- **🪟 Apple 风格毛玻璃悬浮菜单**
  - 任意单集的省略号（`...`）按钮均支持**双重交互**：鼠标光标长停（约 0.5 秒）自动预览菜单、移走即消失；**左键点击则常驻锁定显示**（悬停态点击可无缝升级为锁定）。
  - 实时背景高斯模糊（`glass.Blur`）半透明菜单，集成 **播放/暂停、插播、添加到队列、收藏/取消收藏、标记为已播完/未播、单集详情、沉浸播放视图、前往节目、复制单集链接**。
- **🎧 纯 Go 内置全能音频引擎 & 记忆播放**
  - 无需安装 FFmpeg 或任何系统解码包，内置支持 **MP3、M4A / AAC（AAC-LC / HE-AAC v1 SBR / HE-AAC v2 PS）、OGG Vorbis、FLAC、WAV** 边下边播。
  - 支持 **WSOLA 变速不变调**（`0.75x / 1x / 1.25x / 1.5x / 1.75x / 2x`）、Windows 系统媒体控制中心（SMTC）与键盘多媒体键联动、系统代理自动适配。
  - 关闭应用自动记忆当前单集、播放队列、精确播放位置、音量、倍速及上次停留页面，重新打开无缝续听。

---

## 📦 下载与安装

前往本仓库的 **[GitHub Releases](https://github.com/kelsilib/my-podcasts/releases)** 页面，根据你的使用习惯选择以下任一发行版（仅支持 64 位 Windows 10 / 11）：

| 发行版本 | 文件名示例 | 特点与适用场景 |
| :--- | :--- | :--- |
| **安装版 (Setup)** | `my-podcasts-setup-alpha0.0.1-windows-amd64.exe` | 标准 Windows 安装向导（NSIS），默认用户级安装至 `%LOCALAPPDATA%\Programs`，自动创建开始菜单与桌面快捷方式，支持在「Windows 设置 → 应用」中一键卸载。 |
| **便携版 (Portable)** | `my-podcasts-portable-alpha0.0.1-windows-amd64.zip` | 免安装，解压到任意文件夹或 U 盘即可运行 `My Podcasts.exe`。自带 `portable.txt` 标记，配置与缓存全部保存在同目录 `data\` 内（删除 `portable.txt` 则切回系统 `%APPDATA%`）。 |

> **系统依赖提示**：Windows 10 / 11 通常已预装 **Microsoft Edge WebView2 Runtime**（仅在点击「登录 Apple ID」时使用）；若系统精简版缺失该组件，可从 [Microsoft 官网](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) 安装 Evergreen Runtime。不登录也可直接收听全部公开播客与榜单。

> **校验安装包完整性（可选）**：
> ```powershell
> Get-FileHash .\my-podcasts-setup-alpha0.0.1-windows-amd64.exe -Algorithm SHA256
> ```
> 将输出的哈希值与 Release 附带的 `SHA256SUMS.txt` 对比一致即可。

---

## 🛠️ 自行编译与打包教程

本项目为 **100% 纯 Go（`CGO_ENABLED=0`）** 工程，**不需要安装 GCC / MinGW / Node.js / Visual Studio**，只需安装 Go 即可完成从源码编译到生成安装包的全流程。

### 1. 环境准备

- **操作系统**：Windows 10 / 11（64 位）
- **Go 工具链**：**Go 1.27.3 或更高版本**（与 [`go.mod`](go.mod) 保持一致），从 [go.dev/dl](https://go.dev/dl/) 下载安装并确保 `go` 命令在 `PATH` 中可用。
- **Git**（用于克隆代码）：从 [git-scm.com](https://git-scm.com/) 安装。

验证环境：

```powershell
go version
# 预期输出类似：go version go1.27.4 windows/amd64
```

### 2. 获取源码并下载依赖

```powershell
git clone https://github.com/kelsilib/my-podcasts.git
cd my-podcasts
go mod download
```

> 若国内网络拉取 Go 模块较慢，可先配置代理：
> ```powershell
> go env -w GOPROXY=https://goproxy.cn,direct
> ```

### 3. 本地运行与单元测试

```powershell
$env:CGO_ENABLED = "0"

# 静态检查与全量测试（包含无头 UI 渲染与交互测试）
go vet ./...
go test -count=1 -v ./...

# 直接从源码启动应用
go run .
```

### 4. 快速编译单个可执行文件（开发/调试用）

```powershell
$env:CGO_ENABLED = "0"

# 无控制台黑框的 Windows GUI 版本
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "My Podcasts.exe" .
```

### 5. 一键封装发行版（生成 Setup 安装包 + Portable 便携版 ZIP）

仓库已内置一键封装脚本 [`scripts/package-windows.ps1`](scripts/package-windows.ps1)，底层调用 `mygo build`：
- 自动将 [`resources/icon.png`](resources/icon.png) 编译为多分辨率 Windows 图标、高 DPI Manifest 与版本信息并嵌入 `.exe`；
- 在 Windows 上自动按需下载并调用 **NSIS** 生成标准安装程序 `setup.exe`；
- 自动组装包含 `portable.txt` 的绿色便携版 `.zip` 并生成 `SHA256SUMS.txt`。

在仓库根目录打开 PowerShell 执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-windows.ps1
```

可选参数：
- `-SkipTests`：跳过单元测试加快打包
- `-Arch arm64`：交叉编译 Windows ARM64 版本（默认 `amd64`）

构建完成后，全部发行产物位于 `dist\release\` 目录：

```text
dist\release\
├── my-podcasts-setup-alpha0.0.1-windows-amd64.exe     # Windows 安装包
├── my-podcasts-portable-alpha0.0.1-windows-amd64.zip  # 绿色便携版
└── SHA256SUMS.txt                                     # SHA-256 校验文件
```

### 6. GitHub Actions 自动发布

仓库已配置 [`.github/workflows/release.yml`](.github/workflows/release.yml)。推送形如 `alpha0.0.1` 或 `v1.0.0` 的版本 Tag 时，GitHub Actions 会自动在 `windows-latest` 上执行测试、封装 Setup `.exe` 与 Portable `.zip` 并发布到 GitHub Releases：

```powershell
git tag alpha0.0.1
git push origin alpha0.0.1
```

---

## 📁 项目结构

```text
.
├── main.go                       # 程序入口
├── mygo.json                     # MyGo 应用元数据、图标与打包配置
├── resources/icon.png            # 应用主图标源文件
├── scripts/package-windows.ps1   # Windows Setup + Portable 一键封装脚本
├── internal/
│   ├── apple/                    # Apple Podcasts Web API 客户端、进度解析、RSS 文稿解析
│   ├── player/                   # 纯 Go 流媒体下载、音频解码 (MP3/AAC/HE-AAC/OGG/FLAC) 与播放控制
│   └── app/                      # MyGo 原生 UI、毛玻璃菜单、沉浸视图、WebView2 登录、DPAPI 加密与云端同步
├── DEVELOPMENT.md                # 架构演进与开发设计文档
└── LICENSE                       # MIT 开源协议
```

---

## ⚠️ 免责声明

- 本项目为基于开源技术构建的**第三方非官方桌面客户端封装软件**，与 Apple Inc. 没有任何隶属、赞助或授权关系。
- “Apple” 及 “Apple Podcasts” 为 Apple Inc. 的注册商标；所有播客音频流、封面与元数据均来自 Apple Podcasts 公开接口及各播客创作者的公开 RSS 源。
- 本软件仅供个人学习、技术交流与桌面便捷收听使用，请遵守相关平台服务条款并支持正版内容。

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 开源。
