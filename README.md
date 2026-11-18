# O.Arc Drop

运行在 `HostDzire-LA` 上的个人 **无损剪贴板 · 20 MB 自动分片快传（本机 / Silo / Google Drive） · Silo 图片画廊与音乐索引台**，采用 Apple Liquid Glass（浅色默认 + 动态封面取色）界面。

## 功能与边界

- **无损剪贴板（Clipboard）**：支持文本、代码片段与 `⌘V` 粘贴无损截图/小文件，基于 SSE 多端实时同步；任意条目可一键转为公开分享短链。
- **20 MB 分片快传（Drop）**：前端自动按 `20 MiB` 切片（最多 3 路并发、自动重试），无需关闭 Cloudflare 橙云即可突破 100 MB 单次请求限制上传任意体积文件；上传前可选择写入 **本机暂存（支持到期自动销毁）**、**Silo (`aurora` 桶)** 或 **Google Drive（经 `rclone` 隔离目录 `gdrive:OArc-Drop`）**。
- **Silo 画廊与音乐台（Gallery & Music）**：通过内网 S3 协议实时索引 `http://minio:9000` 桶内图片（自动跳过 `thumbs/` 重复项并配对缩略图）与音频（解析 MP3 ID3v2 / FLAC 内嵌封面、采样率位深，并支持同目录或跨目录 `music/lyrics/` 同名 `.lrc` 歌词自动挂载），底部悬浮播放器在切换标签页时不中断播放。
- **公开分享取件页（`/s/<短码>`）**：主工作台 `/` 与 `/api/*` 由 Cloudflare Access 保护；公开分享路由统一收敛在 `/s/*`（含页面、元数据、提取码校验与流式 Range 下载），在 Cloudflare Access 中仅需一条 `/s/*` Bypass 规则即可让外部访客免登录满速无损下载。

## 本地运行与测试

```bash
# 1. 前端类型检查与生产构建
cd web && npm run check && cd ..

# 2. 后端单元与端到端集成测试
go test -v ./...

# 3. 本机一键启动内置演示环境（含模拟 Silo 图库、音乐及 Google Drive 目录）
go run ./cmd/demo
# 浏览器打开 http://127.0.0.1:8095
```

## 线上部署（`HostDzire-LA`）

容器定义见 [deploy/docker-compose.yml](deploy/docker-compose.yml)（加入 `aurora_default` 网络直连 `minio:9000`，内存硬上限 `128 MiB`），由 `/opt/gateway/nginx.conf` 反代 `drop.arcueidblog.top`（注意 SSE 与大文件流关闭 `proxy_buffering`）。

## 发布记录

- **2026-11-18 v1.0.0（已上线 `https://drop.arcueidblog.top`）**：
  - 完成 Go 零外部依赖后端与 Vue 3 Apple Liquid Glass 前端（桌面端 macOS 左栏固定高度一体视窗 + 手机端 iOS 26 全屏安全区适配），本地通过 `go test -v ./...` 与 `npm run check`。
  - 在 `HostDzire-LA` 部署 `oarc-drop-1` 容器（常驻内存实测约 `8.6 MiB`），直连内网 `pgsty/silo` 索引桶内 265 首「歌手 - 歌名」音频（自动配对 262 份 `music/lyrics/` 歌词）与 53 张图像，默认对象投递前缀 `drop/`，连通 Google Drive 隔离目录 `gdrive:OArc-Drop`。
  - 经 Cloudflare MCP 配置 `drop.arcueidblog.top` 橙云解析、Cloudflare Access（主站挂载可复用策略「只允许我」，`/s/*`、`/assets/*`、`/.well-known/acme-challenge/*` 免登 Bypass）及动态缓存绕过规则；签发独立 Let's Encrypt 证书并接入每日 Restic 备份。
