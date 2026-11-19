<div align="center">
  <img src="resources/icon.png" alt="My Podcasts" width="100" height="100" />
  <h1>My Podcasts</h1>
  <p>基于 <a href="https://github.com/egoist/mygo">MyGo</a> 框架打造的 Windows 端 Apple Podcasts 桌面封装客户端</p>
</div>

---

## 这是什么

**My Podcasts** 是一款面向 Windows 的第三方 **Apple Podcasts** 桌面客户端。

依托 **[MyGo](https://github.com/egoist/mygo)** 框架构建，应用体积轻巧、启动迅速，整体界面贴近苹果原生播客与音乐客户端的观感：无边框融合标题栏、跟随系统明暗主题、柔和流畅的界面动效，无需在浏览器标签页里来回切换，打开桌面软件即可专注收听。

---

## 特色功能

- **一键切换地区**
  - 顶部栏可直接切换 **美区（US）/ 国区（CN）/ 日区（JP）/ 英区（GB）**，无需修改系统设置或退出账号，就能浏览不同地区的热门节目、最新单集和搜索结果。
- **登录 Apple ID，跨设备同步进度**
  - 登录你的 Apple ID 后，自动同步 iPhone / Mac 上的**订阅节目、已存单集、待播与继续收听列表、最近收听记录**。
  - 在发现页、待播、收藏、节目详情、单集详情、搜索结果和沉浸视图中，都会同步显示准确的剩余时长与「已播完」状态；在电脑上收听、暂停或拖动进度，也会自动同步回云端。
- **沉浸播放视图与逐字文稿**
  - 点击播放栏封面即可进入单集详情与沉浸播放页面，背景会根据封面自动配色并带有柔和光晕过渡，隐藏底部播放栏，专注呈现大封面与播放控制。
  - 支持展示带时间戳的逐字文稿（随播放高亮，点击任意一句即可跳转）以及单集章节时间轴；没有文稿的单集会清爽呈现居中封面，不强行堆字。
- **毛玻璃悬浮快捷菜单**
  - 任意单集上的省略号（`...`）按钮都支持两种操作方式：鼠标停留片刻自动弹出菜单、移开即消失；左键点击则固定显示菜单。
  - 菜单内可一键完成**播放/暂停、插播、添加到队列、收藏、标记已播完/未播、查看详情、前往节目、复制单集链接**。
- **顺手的日常收听体验**
  - 支持 `0.75x` 到 `2x` 多档倍速播放（保持人声自然不变调）、快退 15 秒 / 快进 30 秒以及键盘媒体键控制。
  - 关闭软件时自动记住上次听到的单集、播放队列、精确播放位置、音量倍速与停留页面，下次打开直接续听。

---

## 下载与安装

前往 **[GitHub Releases](https://github.com/kelsilib/my-podcasts/releases)** 下载（支持 Windows 10 / 11 64 位）：

- **安装版（Setup）**：`my-podcasts-setup-alpha0.0.1-windows-amd64.exe`
  - 标准安装向导，自动创建开始菜单与桌面快捷方式，可在系统「设置 → 应用」中直接卸载。
- **便携版（Portable）**：`my-podcasts-portable-alpha0.0.1-windows-amd64.zip`
  - 无需安装，解压到任意目录即可运行 `My Podcasts.exe`；数据与登录状态默认保存在同级 `data` 文件夹中，方便拷贝带走。

> 不登录 Apple ID 也可以直接收听全部公开播客与榜单；登录 Apple ID 同步云端资料需系统具备 Windows 自带的 Edge WebView2 组件。

---

## 自行编译

如果你希望从源码自行编译或打包，只需先安装 [Go 1.27+](https://go.dev/dl/)，无需额外配置其他编译环境：

```powershell
# 1. 克隆仓库
git clone https://github.com/kelsilib/my-podcasts.git
cd my-podcasts

# 2. 直接运行
go run .

# 3. 单独编译一个可执行文件
go build -ldflags "-s -w -H=windowsgui" -o "My Podcasts.exe" .

# 4. 一键打包安装版（Setup）和便携版（Portable ZIP）
#    生成的文件在 dist\release\ 目录下
powershell -ExecutionPolicy Bypass -File .\scripts\package-windows.ps1
```

---

## 致谢与说明

- 本项目界面与桌面封装基于开源框架 **[MyGo](https://github.com/egoist/mygo)** 实现。
- 本项目为第三方非官方 Apple Podcasts 封装客户端，与 Apple Inc. 没有任何隶属或授权关系，仅供个人学习与日常收听使用。
- 开源协议：[MIT License](LICENSE)。
