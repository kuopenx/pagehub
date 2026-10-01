# Pagehub 0.2.0

一个 Go 常驻进程、一个端口，提供局域网 HTML 页面、Dashboard 和本机 MCP 管理接口。

## 使用

```sh
go build -trimpath -ldflags='-s -w' -o pagehub .
./pagehub --port 8765
```

- Dashboard: `http://电脑局域网IP:8765/`
- 页面: `http://电脑局域网IP:8765/<服务器生成的UUID>/`
- MCP: `http://127.0.0.1:8765/_mcp`，Streamable HTTP，必须携带管理令牌。
- 健康检查: `http://127.0.0.1:8765/_health`
- 私有存储: `~/.pagehub/pages/<uuid>/index.html` 和 `page.json`。
- `~/.pagehub/token` 保存管理令牌，不在 Dashboard 或日志中公开。

页面没有休眠、有效期、数量或业务大小限制。页面内容按需从磁盘读取；内存保留少量元数据。部署时 JSON/HTML 会暂时占用内存，完成后不保留 HTML 引用。日志最多两个约 1 MiB 文件。

## MCP 工具

使用官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`。无状态 HTTP 支持 MCP `2026-07-28`，并兼容 SDK 支持的旧协议。

- `create_page(title, media_type, html)`：`media_type` 固定为 `text/html`。始终新增，生成 UUID，允许同名。
- `update_page(id, title?, media_type?, html?, expected_revision?)`：传入标题或完整替换 HTML。传入 HTML 时必须同时指定 `media_type=text/html`。可选版本号避免覆盖并发修改；省略则无条件更新。保留 UUID、URL 和创建时间。
- `read_page(id, start_line?, end_line?)`：读取源码和版本号；默认全文，可指定从 1 开始、包含首尾的行区间。保留原始换行，不在源码里加入行号。结束行超过文件末尾时截到末尾；开始行超出范围时返回错误。
- `patch_page(id, expected_revision, edits)`：局部替换，`edits` 为 `[{"old_text":"唯一旧文本","new_text":"新文本"}]`。先读取取得版本号；旧文本必须非空且在当时源码中恰好匹配一次，可增加上下文区分重复内容。按顺序执行，每条修改可使用前一条的结果；新文本为空表示删除。不支持正则或模糊匹配。任一匹配失败、版本过期或最终文档无效，都不保存整批修改。
- `list_pages(query?, offset?, limit?)`：标题和 ID 检索，按创建时间倒序；默认每次返回 100 条，`limit=0` 返回全部。分页不限制存储数量。
- `delete_page(id)`：永久删除服务器管理的文件和索引，后续访问返回 404。原始文件不受影响，已经传给浏览器的内容不能撤回。

返回 ID、标题、UTC 创建/更新时间、文件大小、版本号、相对路径、本机 URL 和 LAN URL。新页面和旧版页面从 revision=1 开始；每次成功 update/patch 增加一次并持久化。版本冲突后重新读取并检查修改，不要直接用新版本号盲目重试。手机优先使用与电脑所在 Wi-Fi 相符的 LAN URL。VPN/虚拟网卡可能产生额外地址。

局部修改示例（两次 MCP 调用）：

```json
{"id":"服务器返回的 UUID","start_line":80,"end_line":100}
```

将 `read_page` 返回的 `page.revision` 传给 `patch_page`：

```json
{"id":"服务器返回的 UUID","expected_revision":1,"edits":[{"old_text":"<h1>旧标题</h1>","new_text":"<h1>新标题</h1>"}]}
```

### 资源约束

只接受包含 `<html>` 根元素的完整 UTF-8 HTML 文档文本。允许内嵌 CSS、JavaScript、SVG，以及 data URL 资源。不接受本地路径、远程 URL 导入、Markdown/PDF/ZIP、多文件上传或独立图片/CSS/JS。Schema 拒绝额外参数和其他媒体类型。

调用者负责生成自包含页面。浏览器可能请求 HTML 中引用的外部资源，但服务器不会收集、下载或托管这些依赖。允许内嵌 JavaScript，不做 HTML 消毒；这是面向用户自己生成页面的工具。

## 增删改与重启

管理写入串行处理。先在隐藏临时目录写入完整内容和元数据，再发布；更新使用短暂的备份目录，成功后立即删除备份。读取先打开文件，避免读到半写入内容。服务启动时恢复中断更新并清理临时/删除目录，从各页面的元数据重建索引。

管理成功后立即生效，无需重启、文件监视器或每页独立进程。Dashboard 和页面手动刷新即可看到变化；服务通过 Cache-Control 与 ETag 防止刷新后旧内容滞留。Dashboard 时间按查看设备的本地时区显示，标题按 HTML 转义处理。

## macOS 后台启动

安装位置：`~/.pagehub/bin/pagehub`。

用户级 LaunchAgent：`~/Library/LaunchAgents/com.garyshu.pagehub.plist`。用户登录 macOS 后自动启动，并在异常退出后恢复；不是尚未登录就运行的系统 LaunchDaemon。

若 macOS 防火墙尚未允许新安装的程序入站，可在电脑终端执行 `./allow-lan.command`，输入管理员密码。这只添加 Pagehub 的应用许可，不关闭防火墙。

安装后台服务与接入 Codex：

```sh
python3 install-macos.py
python3 configure-codex.py
```

Codex 配置保存 Streamable HTTP 地址及私有 Authorization 头，配置文件权限设为 0600。若当前 Codex 会话尚未显示六个工具，重新打开 Codex 以刷新工具清单。

接入本机 Claude Code CLI（用户级，所有项目可用）：

```sh
python3 configure-claude.py
```

使用同一个 `http://127.0.0.1:8765/_mcp` 后台服务及现有令牌，保存到 `~/.claude.json` 的 `mcpServers.pagehub`，不启动第二个服务器。可用 `claude mcp get pagehub` 检查连接；已有 Claude 会话重新启动后加载配置。

```sh
launchctl print gui/$(id -u)/com.garyshu.pagehub
launchctl kickstart -k gui/$(id -u)/com.garyshu.pagehub
```

MCP 管理入口只接受 loopback 请求、合法本机 Host/Origin 和 Bearer token。网页和 Dashboard 可通过局域网访问。默认监听 IPv4 的 8765 端口。

## 验证

```sh
go test -race ./...
go vet ./...
```

测试覆盖同名 ID 唯一性、创建/读取/更新/局部修改/搜索/删除、精确行读取和超长行、整批失败回滚、重叠匹配拒绝、并发版本冲突、旧页面兼容和版本持久化、崩溃恢复、输入 Schema、超过 SDK 默认 4 MiB 的上传、Dashboard 转义、缓存刷新以及管理访问隔离。
