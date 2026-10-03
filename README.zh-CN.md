# Pagehub

[English](README.md) · [简体中文](README.zh-CN.md)

**把 AI 生成的单文件 HTML，变成手机上可以直接打开的页面。**

一个 Go 二进制、一个后台进程、一个端口，提供 LAN 页面、Dashboard 和本机 MCP。Codex、Claude Code 等客户端直接提交 HTML 或局部修改，无需知道页面保存在什么位置。

适合自用 artifact、SVG 动画、交互演示和可视化。页面允许同名，使用 UUID 区分；没有休眠、到期、数量或业务大小配额。当前版本 **0.4.0**，采用 [MIT](LICENSE) 许可。[发布说明](docs/releases/v0.4.0.zh-CN.md)。

## 安装

Pagehub **只分发源码**，支持 Homebrew、`go install` 和本地源码构建。GitHub Release 保留版本说明和自动生成的源码归档，不提供预编译二进制或安装包。

**macOS 和 Linux 均支持后台安装与登录自启动。** 不依赖 Python 或 Node.js。

### Homebrew（macOS）

```sh
brew install kuopenx/tap/pagehub
pagehub setup
pagehub connect codex    # 或 pagehub connect claude
pagehub doctor
pagehub open
```

Formula 从版本化源码构建，Go 由 Homebrew 管理。`setup` 安装的是用户级 LaunchAgent，登录后自动启动，不需要 root。可重复执行，用于安装或更新已有后台实例。

### Go（macOS 和 Linux）

需要 Go 1.26.0+：

```sh
go install github.com/kuopenx/pagehub/cmd/pagehub@latest
```

确认 `GOBIN`，或未设置 `GOBIN` 时的 `$(go env GOPATH)/bin`，已加入 `PATH`。两个平台均执行 `pagehub setup`，然后用 `pagehub connect codex` 或 `pagehub connect claude` 连接客户端。需要前台运行时使用 `pagehub serve`。

源码构建与验证见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)。

### Linux 后台服务

`pagehub setup` 检测到 Linux 后，将用户级 systemd unit 安装到 `~/.config/systemd/user/io.pagehub.agent.service`（设置绝对路径 `XDG_CONFIG_HOME` 时使用该目录）。安装后立即启动，并启用 systemd 用户会话启动时的自启动，通常是在登录后。以普通用户执行，不要使用 `sudo`。Linux 需要 systemd 240+ 和可用的用户会话/bus；没有用户级 systemd 的系统仍可使用 `pagehub serve`。

如果需要在登录前随开机启动、退出登录后继续运行，可自行执行 `loginctl enable-linger "$USER"`（可能需要授权）。Pagehub 不修改 linger 设置，参见 [systemd loginctl](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)。

`pagehub service start/stop/restart/status` 在 Linux 上使用 `systemctl --user`，在 macOS 上使用 launchd。停止 Linux 服务不会关闭登录自启动；卸载会关闭自启动并移除本实例的 unit，同时保留数据。Setup 拒绝其他安装的 unit 和 drop-in，升级失败会恢复旧可执行文件、设置、unit、运行状态与自启动状态。Linux JSON 服务状态在启用登录自启动时包含 `enabled: true`。

目前仅支持 macOS 和 Linux。

## 使用

本机 Dashboard：<http://127.0.0.1:8765/>。手机与电脑连接同一局域网，打开 `http://<电脑局域网IPv4>:8765/`；`setup`、`doctor` 和 `open` 会输出 LAN 链接。

通过 MCP 让 AI 创建页面，随后使用返回的页面链接，或者在 Dashboard 中搜索和打开页面。创建、更新和删除即时生效；已打开的 Dashboard 会自动检测变化并提示，点击即可原地刷新列表。搜索随输入即时过滤（按 `/` 聚焦搜索框，回车直接打开第一个结果），可按最近更新、最近创建或标题排序；搜索词和排序会保留在网址中。点击卡片任意位置即可在当前标签页打开页面，用浏览器返回键回到列表。在本机上，复制链接会复制局域网地址，扫码按钮会显示二维码，方便用手机打开。Dashboard 跟随系统浅色或深色外观，禁用 JavaScript 时仍可使用。

| 命令 | 行为 |
| --- | --- |
| `pagehub serve` | 前台运行 HTTP 和 MCP |
| `pagehub setup` | 安装或升级 macOS/Linux 后台服务，启动并等待健康检查 |
| `pagehub service start/stop/restart/status` | 管理用户级后台服务 |
| `pagehub connect codex/claude` | 注册本服务，保留其他客户端配置 |
| `pagehub disconnect codex/claude` | 移除指向本服务的 Pagehub 注册 |
| `pagehub doctor` | 检查后台、HTTP、认证 MCP、客户端配置和 LAN 地址 |
| `pagehub open` | 打开 Dashboard；无浏览器时仍输出链接 |
| `pagehub version` | 程序版本、提交和构建时间 |
| `pagehub uninstall` | 移除后台注册及托管二进制，保留页面、令牌、设置和客户端配置 |

每条命令支持 `--help`。`--json` 提供机器可读结果；成功退出码为 0，操作失败为 1，参数解析或校验失败为 2。错误写入 stderr。`--data-dir`、`--port`、`--service-name` 可覆盖保存的设置；连接命令支持 `--config-file` 指定配置文件。

在 `setup` 时修改 `--service-name`，会迁移该数据目录记录的服务；新进程通过健康检查后删除旧服务文件，启动失败则恢复原安装。健康检查会将 Pagehub 进程与 launchd 或 systemd 报告的 PID 核对。即使保存的设置损坏，`version` 仍能正常输出。

服务默认监听 `0.0.0.0:8765` 的 IPv4。不同实例必须使用不同端口、数据目录和 service name。开发实例示例：

```sh
pagehub serve --port 8766 --data-dir "$(mktemp -d)"
```

### 接入客户端

`connect` 为本机 HTTP 地址配置 Authorization 头；令牌不出现在命令输出中。同名 MCP 指向其他地址时拒绝覆盖。配置文件权限设为 `0600`，已有客户端会话可能需要重新打开，以刷新六个工具。

自定义端口或目录时，为 `setup`、`connect` 使用同一个 `--data-dir`，连接地址会读取保存的端口。配置路径默认为 `~/.codex/config.toml` 和 `~/.claude.json`；也可通过 `--config-file` 指定客户端使用的其他配置。

### 升级与卸载

```sh
brew upgrade pagehub
pagehub setup
pagehub doctor
```

Go 安装用户再次执行 `go install github.com/kuopenx/pagehub/cmd/pagehub@latest`，然后在 macOS 或 Linux 上执行 `pagehub setup` 和 `pagehub doctor`。前台实例需要重启以使用新可执行文件。

`setup` 将当前版本复制到 `~/.pagehub/bin/pagehub`，不会覆盖包管理器的文件。启动失败时回滚之前的二进制、设置和服务注册。现有页面、URL、令牌和修订号保持不变；旧版 launchd 标签迁移至 `io.pagehub.agent`。

卸载后台服务并保留页面：

```sh
pagehub disconnect codex
pagehub disconnect claude
pagehub uninstall
brew uninstall pagehub   # 如果用 Homebrew 安装
```

页面数据不会自动删除。管理令牌首次随机生成后持久化，重启与升级不会使其失效。

## MCP 接口

HTTP 与 MCP 共用端口，管理地址为 `http://127.0.0.1:8765/_mcp`。使用官方 Go SDK 的无状态 Streamable HTTP，支持 SDK 接受的协议版本；客户端负责协商。

| 工具 | 参数 | 行为 |
| --- | --- | --- |
| `create_page` | `title, media_type, html` | 完整 HTML 文本；媒体类型固定为 `text/html`，生成 UUID |
| `list_pages` | `query?, offset?, limit?` | 按标题/UUID 检索，创建时间倒序；默认 100 条，0 表示全部 |
| `read_page` | `id, start_line?, end_line?` | 精确源码和修订号；1 起始、首尾包含的行区间 |
| `patch_page` | `id, expected_revision, edits` | 唯一匹配的精确文本替换，整批成功才保存 |
| `update_page` | `id, title?, media_type?, html?, expected_revision?` | 重命名或完整替换，版本检查可选 |
| `delete_page` | `id` | 删除托管文件与内存索引，后续 URL 返回 404 |

工具参数与用法见 [MCP 示例](docs/mcp.zh-CN.md)，完整 HTML 示例见 [鹈鹕骑自行车](examples/pelican-bicycle.html)。修订号保护并发修改，不保存历史副本。

## 内容与访问边界

只托管完整 UTF-8 单文件 HTML，可内嵌 CSS、JS、SVG、data URL。不接受路径、URL 导入、PDF、ZIP 或独立资源文件；外部引用仍可能由浏览器请求，但服务器不会下载或代管。

Dashboard 和页面允许 LAN 访问，无登录；MCP 管理仅接受 loopback、合法 Host/Origin 和 Bearer token。HTML 可以执行 JavaScript，所有页面目前共享同一 origin，因此只用于可信内容和可信局域网。详细边界及私密漏洞反馈见 [SECURITY.md](SECURITY.zh-CN.md)。

内存索引只保留元数据；正文按需读取，操作期间暂时占用内存。实际容量受磁盘和内存限制。日志约 1 MiB 轮转，最多两个文件，不记录 HTML 或认证信息。

## 存储与排查

默认数据目录 `~/.pagehub` 包含 `settings.json`、`token`、日志、`bin/pagehub` 和 `pages/<uuid>/{index.html,page.json}`。目录由服务管理，MCP 调用者不需要访问它。

先运行 `pagehub doctor --json`：

| 问题 | 检查 |
| --- | --- |
| 服务未启动 | `pagehub service status`；`pagehub service start`；检查 `pagehub.log` |
| 本机可访问、手机不可访问 | 同一 Wi-Fi、正确 IPv4、入站防火墙许可（macOS 系统设置或 Linux 防火墙） |
| MCP 401 | 用同一数据目录重新执行 `pagehub connect <client>` |
| MCP 403 | 使用本机管理 URL，检查 Host/Origin；LAN 仅用于页面访问 |
| 新工具未出现 | 重新打开客户端会话 |
| 补丁匹配失败或版本冲突 | 重新读取页面，检查上下文与最新修订号 |

## 维护

开发规则见 [AGENTS.zh-CN.md](AGENTS.zh-CN.md)，源码发布流程见 [docs/releasing.zh-CN.md](docs/releasing.zh-CN.md)。由 [kuopenx](https://github.com/kuopenx) 维护；问题和建议可提交到 [Issues](https://github.com/kuopenx/pagehub/issues)。请提供版本、复现步骤与脱敏诊断。

依赖版权声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
