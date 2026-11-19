# Apple Podcasts Windows Native Client — 开发与交接文档 (`gpt6.2astra` Review 指南)

本文档完整总结 **My Podcasts（Apple Podcasts Windows 原生客户端）** 的开发演进、架构设计理念、关键技术难点的根因与解法、并发/渲染不变量，以及供 **`gpt6.2astra`** 进行深度 Code Review 与后续缺陷修复的清单。

---

## 1. 项目概览与构建环境

- **目标**：使用纯 Go + 原生桌面 UI 框架 `github.com/egoist/mygo` 构建具备 macOS Apple Podcasts / Apple Music 质感与沉浸交互的 Windows 原生播客客户端（不使用常驻内嵌浏览器套壳渲染主界面）。
- **技术栈**：
  - Go `1.27.3`（当前工作机工具链位于 `$env:USERPROFILE\sdk\go1.27.4\go\bin`）
  - **严格 `CGO_ENABLED=0`**（零 C 依赖交叉/本地纯 Go 构建）
  - UI 框架：`github.com/egoist/mygo v0.10.0`（声明式即时模式布局、`Match`/`Fly` 共享元素动画、`Timeline` 编排、`AnimatePose`、系统媒体控制 `mygo.NowPlaying`、`mygo.Secrets`、按需临时 WebView2 鉴权窗口）
  - 音频解码与流式播放：
    - 容器/编码：`mygo/audio`（WSOLA 变速不变调 `SetPreservesPitch(true)`）、`mygo/audio/mp4`、`github.com/hajimehoshi/go-mp3 v0.3.4`
    - Windows 系统解码桥接：`internal/player/mfaac_windows.go` 通过 `ole32.dll` / `mfplat.dll` 原生 `syscall` 调用 Media Foundation `CMSAACDecMFT` 解码 **HE-AAC / HE-AAC v2**
- **常用构建与测试命令（PowerShell）**：
  ```powershell
  $env:PATH = "$env:USERPROFILE\sdk\go1.27.4\go\bin;" + $env:PATH
  $env:CGO_ENABLED = "0"
  go vet ./...
  go test -count=1 -v ./...
  go build -o apple-podcasts.exe .
  ```
  > 注：若当前桌面已有正在运行的旧版程序进程，Windows 会将旧镜像重命名为 `apple-podcasts.exe~` 并拒绝删除，关闭旧窗口后即可正常清理。

---

## 2. 完整开发演进历程

整个项目按以下六个阶段迭代演进：

1. **阶段一：技术调研与方案选型（方案 A）**
   - 调研 `mygo` 架构（纯 Go UI 引擎 + `netproxy` + WebView2 `Session` + `audio` WSOLA）与 Apple Podcasts Web 端接口（`amp-api.podcasts.apple.com`、`podcasts.apple.com` 内嵌开发者 JWT）。
   - 确定**方案 A**：主界面与播放器完全用 `mygo/ui` 原生实现；仅在需要登录 Apple ID 同步云端订阅/待播队列/进度时，按需弹出轻量 WebView2 窗口拦截 `media-user-token` 与 Cookie，登录完成立即销毁浏览器进程。
2. **阶段二：核心客户端脚手架与音频解码攻坚**
   - 实现目录发现、搜索、节目/单集浏览、多地区商店切换（`us` / `cn` / `jp` / `gb`）、WebView2 Apple ID 登录、云端同步与系统媒体按键控制。
   - **关键音频修复**：实测发现大量中文与海外播客使用 HE-AAC / 隐式 SBR `< 24kHz` `.m4a` 流，纯 Go `tphakala/go-aac` 直接报错（截图报错 `go-aac/pcm: unsupported stream`）。通过 `mfaac_windows.go` 注册 `"mp4a"` 编解码器并调用 Windows 原生 `CMSAACDecMFT`，配合 `fallbackAACCodec` 动态降级与 `stream.go` 边下边播临时文件缓冲池（支持 MP3 ID3 估算 Seek 与多格式容器 Seek），彻底解决播客无法播放的问题。
3. **阶段三：视觉精致化与官网布局对齐**
   - 绘制仿 Apple Podcasts 紫渐变波形图标（`resources/icon.png` / `internal/app/icon.png`）。
   - 将顶部调整为无独立 Logo 占据空间、贴合 `mygo` 沉浸标题栏风格的导航区（`topTitleBar`）；发现页改为紧凑官网风（最新单集 Hero 特殊封面卡、继续收听、热门节目网格、热门单集双列排行）。
   - 压缩底部播放 Bar 高度并将标题与封面严格左对齐；新增单集详情页与对标 macOS Apple Music 歌词页的沉浸式全屏视图。
4. **阶段四：真实反馈修复（特殊封面遮挡、假文稿与沉浸控件比例）**
   - 修复 `episodeHeroCard` 特殊封面下方文字/按钮被渐变遮挡消失的 Bug（根因见第 4 节 `mygo/ui` 绘制顺序陷阱）。
   - 修复 CDN `320x320bb.jpg` / `PodcastProtectedASource` 403 灰块问题（统一标准 600 尺寸与 `PodcastProtectedASource` -> `500x500bb.webp`）。
   - 严格区分**真实逐字文稿（`<podcast:transcript>`）**与**普通简介章节**（无正式文稿时不再硬凑假歌词，沉浸视图居中展示大号 `336×336` 封面与放大版控制区，且沉浸模式下隐藏底部播放 Bar）。
5. **阶段五：沉浸视图物理动效与全应用交互顺滑化**
   - 为进入/退出沉浸视图引入 `mygo` 物理弹簧体系：`Match` + `Fly` 共享封面飞跃、底部播放 Bar 滑出与工作区 `Height` 自适应、背景高斯模糊与梯度消融、`Timeline` 错峰入场、`imm-split` <-> `imm-player` 居中/分栏丝滑切换、30fps 仅重绘氛围光斑以及全应用悬停/按压 `AnimatePose` 与 `ContentTransition`。
6. **阶段六：关闭再打开的完整播放状态记忆 + 首次代码自检修复**
   - 新增 `SavedPlayback` 与 `SavedRoute` 持久化模型；支持桌面端冷启动静默断点恢复（记住单集、前后 40 集队列、精确秒级进度、倍速、音量与退出前页面/沉浸态）；在播放停顿、未点播放前预拖动进度/快进退、以及窗口 `OnClose` / `OnBeforeQuit` 同步落盘链路中修复异步丢失与并发竞争问题。

---

## 3. 核心设计理念与不可轻易打破的不变量（Invariants）

`gpt6.2astra` 在重构或修 Bug 时，请务必遵守以下由实测与阅读 `mygo@v0.10.0` 源码确认的底层约束：

### 3.1 `mygo/ui` 层级绘制与过渡陷阱
1. **`.Absolute()` 节点永远在同 `ZIndex` 普通流节点之后绘制（覆盖其上）**
   - `mygo/ui/paint.go` 中普通文档流（`flagAbsolute == 0`）先绘，绝对定位子节点后绘。
   - 因此像 `episodeHeroCard`、`episodeTranscriptView` 这类带渐变遮罩/背景模糊的容器，必须显式让背景层停留在 `ZIndex(0)`、前景文字与交互按钮位于 `.Absolute().Fill().ZIndex(1)`，否则渐变会把文字整层吞没。
2. **`ElementTransition.Enter` 仅当父节点上一帧已存在时才会触发**
   - 若整页被替换挂载，子控件自身的 `Enter` 不会自动运行。
   - 解法见 `contentView`（持久 `c.Key("content-stage")`）与 `episodeTranscriptView`（使用 `Timeline` + `immRun` 触发错峰进场；且当 `immRun == 0` 如直接路由恢复/单测冷启动时跳过初始 `0` 透明度等待，首帧立即可见）。
3. **同一帧内最多只能有一个元素声明同一个 `Match` 标识**
   - `Match` + `Fly: true` 在前/后帧各只能匹配单一源与单一目标。因此 `playerBarView` 中仅当 `!(a.route.page == pageEpisode && a.route.epID == ep.ID && !a.route.transcript)` 时才为 `bar-cover-<epID>` 挂载 `Match: artworkHeroMatch{ep.ID}`，避免与 `episodeDetailPage` 的 `ep-detail-cover-<epID>` 抢占同一 ID。
4. **仅重绘动画与整棵视图重建分离**
   - 悬停/按压态微交互使用 `AnimatePose`（仅 Paint 阶段应用变换，不重建 Go UI 树）；沉浸背景呼吸光晕写在 `Painter.Draw` 中并用 `pt.AnimationFrameRate(30)` 限频且遵守 `c.Preferences().ReduceMotion`。

### 3.2 Apple Podcasts API 与 CDN 规则
- **封面 URL 模板解析（`internal/apple/types.go`）**：
  - 标准封面默认请求 `600` 尺寸（US 榜单实测 40/40 通过，部分尺寸如 `320` 会返回 `403`）；`FormatArtwork(..., 300)` 仍保持输出 `300x300bb.jpg` 兼容契约。
  - 含 `PodcastProtectedASource` 且请求尺寸 `>= 300` 时强制转为 `500x500bb.webp`。
  - 特殊竖版封面 `showPageTall` 由 `FormatHeroArtwork` 解析为 `450x600bb.webp`。
- **文稿 vs 章节严格分离**：
  - `EpisodeTranscript.HasTranscript()` 仅当 `Source == "podcast:transcript" && len(Lines) > 0` 为真。
  - 从 HTML/纯文本简介解析出的时间戳条目（`extractChapters`）存放在 `episodeDetailState.chapters`，只在单集详情页显示为可跳转时间轴，**绝不**伪装成逐字文稿喂给沉浸视图右侧面板。

### 3.3 音频流与 Media Foundation COM 解码规则
- `download`（`internal/player/stream.go`）使用引用计数（`users = 2`：HTTP 读协程 + 解码/关闭方），优先写临时文件 `ap-stream-*.tmp`；MP3 刻意通过不实现 `io.Seeker` 的 `downloadReader` 使 `go-mp3` 即时起播，再由 `mp3Stream.SetPosition` 基于 bitrate/ID3 估算帧定位；其余 M4A/MP4/FLAC/OGG/WAV 使用 `downloadReadSeeker`。
- `mfAACCodec`（`internal/player/mfaac_windows.go`）向 Windows `CMSAACDecMFT` 注入 `HEAACWAVEINFO`（12 字节头 + `AudioSpecificConfig`），在 `mfETransformChange` 时重新协商输出格式（32-bit float 优先，降级支持 16/32-bit PCM）。

### 3.4 状态持久化与崩溃/退出一致性
- Windows 下关闭窗口先触发 `win.OnClose`，所有窗口关闭后 `OnWindowAllClosed` 触发 `App.Quit()` -> `OnBeforeQuit`。
- `win.Update(...)` 在窗口销毁链路中不会同步执行，因此 `handleProgressUpdate`、`maybeSyncPeriodicProgress`（每 3 秒本地 / 15 秒云端）和 `persistSessionNow` 全部**在 `a.stateMu` 保护下直接同步执行原子文件写入（`atomicWriteFile`）**，禁止将关键落盘逻辑延迟到 UI 回调队列中。
- `compactPlaybackQueue` 将持久化队列裁剪为以当前集为中心的最多 40 集窗口，并剥离非当前集的冗余 `FullDescription` HTML，防止 `state.json` 膨胀。
- 后台播放回调在状态锁内更新进度，界面通过同一把锁读进度。路由由 UI 线程记录，音量与倍速修改也在状态锁内落盘。
- Windows 登录凭据按当前 Windows 用户通过 DPAPI 加密到 `auth.dpapi`。首次读取旧凭据时，应用在加密写入成功后清理旧 `auth.json`。云端订阅和收藏按登录会话缓存，刷新成功后以服务端列表为准。

---

## 4. 模块与文件索引

| 文件路径 | 核心职责与关键符号 |
| --- | --- |
| `main.go` / `mygo.json` | 程序入口调用 `app.Run()`；应用 ID、名称与图标资源声明 |
| `internal/apple/types.go` | 数据模型 (`Show`, `Episode`, `EpisodeTranscript`)、`HasTranscript`、`FormatArtwork`、`FormatHeroArtwork`、`ParseHTMLParagraphs`、`ParseTranscriptFromText` |
| `internal/apple/client.go` | Apple REST 客户端 (`Charts`, `Search`, `GetShow`, `GetEpisodeDetail`, `GetEpisodesPage`, 云端订阅/收藏/进度 API)、`RefreshBearerToken`、`FetchEpisodeTranscript`、`pickBestTranscriptTag` |
| `internal/apple/client_test.go` | 封面模板单测、RSS 文稿标签优先级与相对 URL 解析单测、线上目录 API 集成测试 |
| `internal/player/player.go` | 播放器状态机 (`PlayEpisode`, `Restore`, `Play`, `Pause`, `Stop`, `Seek`, `Skip`, `Next`, `Prev`, `CycleSpeed`, `SetVolume`, `UpdateCurrentEpisode`, `Snapshot`) 与 `mygo.NowPlaying` 系统媒体集成 |
| `internal/player/stream.go` | 边下边播临时文件缓冲 (`download`, `downloadReader`, `downloadReadSeeker`)、容器探测 (`openHTTPStream`)、渐进式 MP3 帧定位 (`mp3Stream`) |
| `internal/player/mfaac_windows.go` | Windows Media Foundation COM MFT (`mfAACCodec`, `fallbackAACCodec`, `appendDecodedSamples`) |
| `internal/player/stream_test.go` | 线上 HE-AAC `.m4a` 流解复用 + Seek 测试、`download` 并发关闭与 PCM 边界单测 |
| `internal/app/state.go` | `AppState`, `EpisodeProgress`, `SavedPlayback`, `SavedRoute`, `loadAppState`, `saveAppState`, `loadCredentials`, `saveCredentials`, `atomicWriteFile` |
| `internal/app/auth.go` | WebView2 临时登录窗 (`startWebSignIn`), Cookie 轮询提取, `signOut` |
| `internal/app/app.go` | 应用协调器 `App`、路由与历史栈、`restoreSession`, `persistSessionNow`, `ensureEpisodeLoaded` (`fetched` 防重与补拉文稿)、`loadCharts`, `handleProgressUpdate`, `maybeSyncPeriodicProgress` |
| `internal/app/view.go` | 全部 `mygo/ui` 声明式视图与动效 (`view`, `contentView`, `discoverPage`, `episodeHeroCard`, `episodeDetailPage`, `episodeTranscriptView`, `immersivePlayerColumn`, `playerBarView`, `scrubberWithColors`, `applyTapPose`) |
| `internal/app/view_test.go` | `ui.NewTester` 无头渲染与像素级回归测试（特殊封面文字可见性、无文稿沉浸行为、开关沉浸动画步进、冷启动记忆恢复） |

---

## 5. 当前已完成的自检修复清单

在生成本文档前已主动 Review 全仓库并修复以下 4 类实测问题（所有对应测试已通过）：

1. **`internal/player/stream.go` 并发关闭与空取消函数修复**：
   - 修复 `download.writeAt` 在 `d.mu` 锁外读取 `d.file` 与 `release()` 置空文件指针之间的数据竞争；增加 `d.closed` 检查防止关闭后无效分配内存缓存。
   - 修复 `download.Close()` 未判空 `d.cancel` 导致的空指针解引用 Panic。
2. **`internal/player/mfaac_windows.go` PCM 采样边界修复**：
   - 修复 `appendDecodedSamples` 在 `raw` 短于 4 字节时仍取 `&raw[0]` 越界的问题，并补充 32-bit 整型 PCM 解码支持。
3. **`internal/apple/client.go` 令牌刷新并发去重与 RSS 文稿解析增强**：
   - 新增 `refreshMu` 防止多并发请求 401 时重复抓取 `podcasts.apple.com` JS 包。
   - `fetchRSSPodcastTranscript` 将 RSS 抓取上限从 `1MB` 扩至 `8MB`（覆盖长寿播客大型 XML），引入 `pickBestTranscriptTag` 按 `JSON > VTT > SRT > plain > HTML` 优先级选择文稿，并支持基于 `FeedURL` 的相对路径解析与宽松剧集标题包含匹配。
4. **`internal/app/app.go` & `auth.go` 状态互斥与并发竞态加固**：
   - 将 `toggleSubscribe`, `toggleSaveEpisode`, `recordRecentEpisode`, `setStorefront`, `syncCloudLibrary`, `signOut`, `saveState` 的 `a.state` 写入与序列化全部纳入 `a.stateMu` 保护，并保持 `LastRoute` 随保存自动更新。
   - 在 `syncCloudLibrary` 回调内补充 `a.client.HasAuth()` 二次校验，防止退出登录途中回填残留云端列表。

---

## 6. 建议 `gpt6.2astra` 重点 Review 与后续改进的方向

以下是经过梳理的高价值 Review 切入点与已知留白，供下一步精准排查：

1. **进度读写并发回归**
   - `TestPlaybackProgressCanUpdateDuringEpisodeRender` 同时执行播放进度回调和 MyGo 列表渲染，覆盖此前会使进程退出的 map 并发读写。
2. **`internal/app/app.go` 路由状态也应在非播放切换时考虑是否及时落盘**
   - 若用户切换了页面/打开了沉浸视图但尚未触发播放或 `saveState`，在正常点击 `X` 时 `OnClose` -> `persistSessionNow()` 会保存路由；但若遇到系统强制杀进程（TaskKill / 断电），`navigate` / `goBack` / `setTranscriptMode` 目前未主动写入 `LastRoute`（需权衡磁盘写入频率，可加节流防抖或仅保留正常退出保存）。
3. **付费订阅与 HLS（`.m3u8`）音频流支持**
   - 目前仅对 `Episode.AssetURL` 进行 HTTP 流式下载；遇到 Apple Podcasts 订阅专享或仅提供 `HLSURL` 的单集时会直接提示不支持，可评估后续增加 `.m3u8` 分片拉取器（`stream.go`）。
4. **`PodcastProtectedASource` 的 `.jpeg` 模板封面探测与内存缓存策略**
   - 部分国区 `PodcastProtectedASource` `.jpeg` 源在 `500x500bb.webp` 之外可能需尝试 `.jpg` / 原始后缀或其它 CDN 尺寸白名单；另外 `app.shows`、`app.episodes` 内存映射长期运行目前无上限淘汰（可按需加简单容量限制）。
5. **RSS `<podcast:transcript>` 的 `<guid>` 精准匹配与流式 XML 解析**
   - 当前主要依赖 `ep.Name` 大小写无关匹配，若剧集改名则可能失配；此外超大 RSS（>8MB）可用 `xml.NewDecoder` 流式遍历 `<item>` 在命中后立即停止，降低解析内存峰值。
6. **`view()` 中已记忆播放进度自动续播策略**
   - 当前设计重开时默认恢复为**暂停态断点**（需点击播放才拉流），若后续产品需求变为“关闭前若处于播放中则打开时也自动开播”，可在 `restoreSession()` 根据 `pb.WasPlaying` 判断并直接调用 `a.player.PlayEpisode(...)`。
7. **Windows GUI 子系统构建参数检查**
   - 当前 `go build -o apple-podcasts.exe .` 验证编译与单测均正常；正式分发打包前可核对是否需要通过 `-ldflags "-H windowsgui"` 隐藏控制台窗口以及嵌入应用清单/图标资源（可结合 `mygo` CLI `build` 命令确认）。

---

## 7. 当前基线验证记录（交接快照）

最近一次全量检查执行于全部自检修复完成后，命令均返回退出码 `0`：
- `go vet ./...`：通过，无警告。
- `go test -count=1 -v ./...`（全部 9 项测试通过，无失败或跳过；根包 `apple-podcasts` 无测试文件）：
  - `apple-podcasts/internal/app` (`3.364s`): `TestEpisodeHeroCardShowsTextOverSpecialHero`, `TestImmersiveViewTranscriptAndPlayerBarBehavior`, `TestImmersiveViewOpenCloseAnimationChoreography`, `TestPlaybackSessionPersistenceAndRestore`
  - `apple-podcasts/internal/apple` (`2.957s`): `TestFormatArtwork`, `TestLiveCatalogAPI`, `TestPickBestTranscriptTag`
  - `apple-podcasts/internal/player` (`1.743s`): `TestOpenHEAACStream`, `TestDownloadWriteAndAppendSamplesSafety`
- `go build -o apple-podcasts.exe .`：构建成功，产物 `apple-podcasts.exe`（`22424064` 字节）。
