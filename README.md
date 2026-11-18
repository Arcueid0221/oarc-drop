# O.Arc Drop

运行在 `HostDzire-LA` 上的个人 **跨端私密收发台 · PicGo 式 Silo 图床 · 三合一文件柜（Silo / Google Drive / 本机） · 联动歌单音乐厅**，采用 Apple Liquid Glass（冰晶白霜浅色默认 + 动态封面取色）界面。

## 四栏核心结构

1. **收发台（Hub）**：
   - **默认私密不公开**：拖入任意体积大文件（自动 `20 MiB` 三路并发切片直穿 Cloudflare 橙云）、粘贴无损截图或输入文本，实时同步至所有设备并支持直接下载、音视频/PDF 内联预览与「直接复制原图像素到剪贴板」；仅当手动点击分享时才生成对外 `/s/<短码>` 与矢量二维码。
   - **Silo 图床模式**：切到「Silo 图床」后，`⌘V` 粘贴截图或拖入图片直接按干净文件名写入 `images/`（撞名自动递增 `-2`），完成后自动将 `![文件名](https://img.arcueidblog.top/aurora/images/...)` 写回系统剪贴板，卡片提供 `MD` / `URL` / `HTML` 一键复制。
   - **手机端一触收发**：内置「粘贴剪贴板」与「复制最新」快捷按钮，解决 iOS Safari 无物理 `⌘V` 痛点。
2. **文件柜（Vault）**：
   - 统一浏览与管理 **Silo 全格式对象**（画廊网格 / 列表双视图，`images/` 与 `drop/` 目录支持网页端直接改名与删除，博客核心目录带防误删保护锁）、**Google Drive（`gdrive:OArc-Drop` 实时列目录、流式直下回本机与删除）** 与 **本机暂存文件**。
3. **音乐厅（Music）**：
   - 通过内网自动读取 Aurora 博客公开歌单（`默认歌单`、`fusion jazz`、`R&B`、`White Album`、`月姬`、`流行` 等）与专辑封面，支持 **列表循环 / 随机播放 / 单曲循环**、音量控制、iOS 锁屏 `MediaSession` 封面切歌，以及 **262 份跨目录配对的可点击跳进度滚动 `.lrc` 歌词抽屉**。
4. **外链管理（Shares）**：
   - 集中管理对外生成的 `/s/*` 短链（Cloudflare Access 单条 Bypass 放行），支持提取码保护、下载次数/有效期限制、矢量二维码弹窗与随时作废。

## 本地运行与测试

```bash
# 1. 前端类型检查与生产构建
cd web && npm run check && cd ..

# 2. 后端单元与端到端集成测试（含分片上传、图床防撞命名、目录保护锁、二维码生成）
go test -v ./...

# 3. 本机一键启动内置演示环境
go run ./cmd/demo
# 浏览器打开 http://127.0.0.1:8095
```

## 线上部署（`HostDzire-LA`）

容器定义见 [deploy/docker-compose.yml](deploy/docker-compose.yml)（加入 `aurora_default` 网络直连 `minio:9000` 与 `backend:8080`，内存硬上限 `128 MiB`），由 `/opt/gateway/nginx.conf` 反代 `drop.arcueidblog.top`。

## 发布记录

- **2026-11-18 v1.1.0（工作台重做与图床/歌单联动上线）**：
  - 将原 5 个零碎标签精简重构为「收发台 / 文件柜 / 音乐厅 / 外链管理」4 栏结构；默认上传改为私密收发，外链改为按需生成并内置零依赖 SVG 二维码（`/api/qr`）。
  - 新增 PicGo 式 Silo 图床直传模式（干净文件名 + 自动复制 Markdown + `MD`/`URL`/`HTML` 按钮 + 复制原图像素 + 手机一键粘贴剪贴板）。
  - 补齐三合一全格式文件柜：Silo 支持全格式浏览与网页端重命名/删除（博客目录默认加锁保护），Google Drive 支持实时列表、流式下载与删除。
  - 音乐厅内网联动博客 7 个公开歌单与专辑封面，补齐随机/单曲循环、音量条、锁屏控制与可点击跳转的完整滚动 `.lrc` 词谱；线上容器实测常驻内存约 `10.6 MiB`。
- **2026-11-18 v1.0.0（首版上线 `https://drop.arcueidblog.top`）**：
  - 完成 Go 后端与 Vue 3 Apple Liquid Glass 前端，配置 Cloudflare Access、Let's Encrypt 证书与每日 Restic 备份。
