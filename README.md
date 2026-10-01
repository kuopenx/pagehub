# Pagehub

把 AI 生成的单文件 HTML 变成手机上可以直接打开的页面。

Pagehub 是一个 Go 常驻服务：同一个端口提供局域网页面、页面集合 Dashboard 和本机 MCP 接口。Codex、Claude Code 等 MCP 客户端直接提交 HTML 或局部修改，用户在同一 Wi-Fi 下打开链接即可查看。

当前版本：**0.2.0**。适合自用 artifact、SVG 动画、交互演示和可视化。页面允许同名，通过服务器生成的 UUID 区分；没有休眠、有效期、数量或业务大小限制。

## 快速开始

### 环境要求

- Go **1.26.0 或更新版本**，版本要求以 [go.mod](go.mod) 为准。
- Git；私有仓库需要对应的 GitHub 访问权限。
- macOS 后台安装需要 Python 3；Codex 配置脚本使用 `tomllib`，需要 **Python 3.11+**。
- 按需安装 Codex CLI 或 Claude Code CLI，并确保命令位于 `PATH`。

以下命令在仓库根目录运行。首次构建需要下载 Go 模块。

```sh
git clone git@github.com:kuopenx/pagehub.git
cd pagehub
go build -trimpath -ldflags='-s -w' -o pagehub .
./pagehub version
python3 install-macos.py
curl --fail http://127.0.0.1:8765/_health
```

安装脚本将二进制复制到 `~/.pagehub/bin/pagehub`，注册用户级 LaunchAgent；**登录 macOS 后自动启动**，异常退出后恢复。再次运行安装脚本会更新二进制并重启已有服务，保留页面和管理令牌。

本机 Dashboard：<http://127.0.0.1:8765/>。手机使用 `http://<电脑的局域网 IPv4 地址>:8765/`，与电脑连接同一局域网。也可以从 MCP 返回的 `lan_urls` 中选择与当前 Wi-Fi 对应的地址。

若 macOS 防火墙阻止访问，执行下面的命令并输入管理员密码；它只允许 Pagehub 入站，不关闭防火墙：

```sh
./allow-lan.command
```

### 连接 MCP 客户端

先安装并启动服务，再按需执行：

```sh
python3 configure-codex.py
python3 configure-claude.py
```

两个客户端都连接 `http://127.0.0.1:8765/_mcp`，共享已有后台进程。脚本将本机管理令牌写入各自的用户级配置，文件权限为 `0600`。Claude 配置可通过 `claude mcp get pagehub` 检查连接；已有客户端会话可能需要重新打开才能刷新工具清单。

配置脚本使用默认端口和存储位置。若自行更改端口或数据目录，需要相应调整客户端 URL 和令牌配置。

### 前台运行与参数

Go 服务可以前台运行；macOS 自启动与客户端配置脚本是额外的安装便利工具。下面使用独立临时目录和端口，便于开发时避免与已安装服务冲突：

```sh
go build -o pagehub .
./pagehub --port 8766 --data-dir "$(mktemp -d)"
```

| 参数或命令 | 默认值 | 用途 |
| --- | --- | --- |
| `--port` | `8765` | HTTP 页面和 MCP 共用的端口，范围 1–65535 |
| `--data-dir` | `~/.pagehub` | 页面、令牌和日志的私有存储目录 |
| `version` | — | 输出当前程序版本并退出 |

服务监听 `0.0.0.0` 的 IPv4 地址。临时数据目录不会在退出时自动删除。

## MCP 工具

基于官方 [Go SDK](https://github.com/modelcontextprotocol/go-sdk)，依赖版本锁定在 `go.mod` / `go.sum`。使用无状态 Streamable HTTP，支持 MCP `2026-07-28` 及 SDK 支持的旧协议；调用者通常交由客户端 SDK 处理协议协商。

| 工具 | 参数 | 行为 |
| --- | --- | --- |
| `create_page` | `title, media_type, html` | 创建页面，生成 UUID；`media_type` 固定为 `text/html` |
| `list_pages` | `query?, offset?, limit?` | 按标题或 UUID 检索，按创建时间倒序；默认 100 条，`limit=0` 返回全部 |
| `read_page` | `id, start_line?, end_line?` | 读取精确源码及修订号；默认全文，可指定从 1 开始、包含首尾的行区间 |
| `patch_page` | `id, expected_revision, edits` | 顺序执行局部文本替换，整批成功才保存 |
| `update_page` | `id, title?, media_type?, html?, expected_revision?` | 修改标题或完整替换 HTML；替换时同时传 `media_type=text/html` |
| `delete_page` | `id` | 永久删除服务器管理的 HTML、元数据和内存索引，页面 URL 返回 404 |

页面元数据包含 `id`、标题、UTC 创建/更新时间、字节大小、`revision`、相对路径、本机 URL 和 LAN URL。创建、更新及补丁响应还提供 Dashboard URL；`read_page` 额外返回 `content`、`start_line`、`end_line` 和 `total_lines`。标题可以重复，操作页面时必须使用 UUID。

### 创建页面

以下 JSON 是 `create_page` 的工具参数，由 MCP 客户端提交；`html` 是实际文本，不是文件路径：

```json
{
  "title": "你好，Pagehub",
  "media_type": "text/html",
  "html": "<!doctype html><html lang=\"zh-CN\"><meta charset=\"UTF-8\"><title>你好</title><h1>你好，Pagehub</h1></html>"
}
```

创建成功后打开返回的 LAN URL，或者刷新 Dashboard。完整 SVG 示例见 [鹈鹕骑自行车](experiments/pelican-original.html)。

### 局部修改页面

先调用 `read_page`，用创建或检索得到的真实 UUID 替换示例中的值：

```json
{"id":"<page UUID>"}
```

将读取结果的 `page.revision` 作为 `expected_revision`，再调用 `patch_page`：

```json
{
  "id": "<page UUID>",
  "expected_revision": 1,
  "edits": [
    {"old_text":"<h1>你好，Pagehub</h1>","new_text":"<h1>手机上也能看到了</h1>"}
  ]
}
```

- `old_text` 必须非空，并在该次替换前的内容中恰好匹配一次；重复时增加上下文。支持精确文本，不支持正则或模糊匹配。
- 各条修改顺序执行，后面的修改能看到前面的结果；`new_text` 为空表示删除。
- 版本过期、任一匹配失败或最终 HTML 校验失败，整批不保存。冲突后重新读取并检查内容，再决定如何修改。
- 新页面及旧版页面从 `revision=1` 开始，每次成功 `update` / `patch` 增加一次；不保留历史版本副本。
- `update_page` 的版本号是可选参数；省略表示无条件更新。
- 行区间读取保留原始换行；结束行超过末尾时截到末尾，开始行超出范围时返回错误。

更新保留页面 UUID、URL 和创建时间，立即生效；浏览器手动刷新即可查看。不需要重启服务或重新上传整页来完成局部修改。

## 支持的内容与访问边界

只接受包含 `<html>` 根元素的完整 UTF-8 HTML 文档文本。可以内嵌 CSS、JavaScript、SVG 和 data URL。不提供独立资源文件托管，也不接受文件路径、URL 导入、Markdown、PDF、ZIP 或多文件上传。HTML 中的外部引用仍可能由浏览器请求，但 Pagehub 不下载或代管这些资源。

Dashboard 和 HTML 页面供局域网直接访问，不要求登录。MCP 管理入口仅接受 loopback 请求、合法本机 Host/Origin 和 Bearer token。令牌在首次启动时生成，存储在数据目录的 `token` 文件中，后续启动复用；它不是写死在源码中的值。

HTML 中的 JavaScript 可执行，服务不做内容消毒。按自用场景部署在可信局域网，提交自己信任的内容；不适合作为面向公网的多用户托管服务。

## 存储与实现

```text
~/.pagehub/
├── bin/pagehub               # macOS 安装后的二进制
├── token                     # 持久化管理令牌
├── pagehub.log               # 日志，最多保留当前和轮转文件
└── pages/<uuid>/
    ├── index.html            # 单文件页面
    └── page.json             # 元数据及修订号
```

一个进程负责所有页面，内存索引只保留元数据；HTML 在请求时从磁盘读取，创建和补丁处理期间会暂时占用内存。没有业务配额，但实际容量仍受可用磁盘和内存限制。日志按约 1 MiB 轮转，最多两个文件，不记录上传的 HTML 或管理令牌。

写入先暂存完整 HTML 和元数据，再发布。更新期间的备份用于中断恢复，成功后清理；启动时重建索引并处理遗留临时目录。已打开文件的读取不会读到半写入内容。删除只影响服务器管理的副本，不会删除调用者的原文件或撤回浏览器已接收的内容。

| 文件 | 职责 |
| --- | --- |
| [main.go](main.go) | 参数、令牌、监听、退出和日志轮转 |
| [store.go](store.go) | 持久化、索引、修订号、精确读取和原子补丁 |
| [tools.go](tools.go) | MCP 工具、Schema 和响应 |
| [http.go](http.go)、[dashboard.html](dashboard.html) | 页面路由、缓存、管理访问限制与 Dashboard |
| [install-macos.py](install-macos.py) | 用户级 LaunchAgent 安装与更新 |
| [experiments/](experiments/) | 鹈鹕示例与真实 MCP 验收脚本 |

## 开发与验证

在仓库根目录执行：

```sh
go test -race ./...
go vet ./...
```

测试覆盖存储生命周期、中断恢复、并发修改、版本冲突、补丁失败不保存、精确行读取、MCP Schema、超过 SDK 默认 4 MiB 的请求、Dashboard 转义和管理访问隔离。HTTP 集成测试需要允许监听本机临时端口。

可选动画检查需要 Node.js：

```sh
python3 experiments/prepare-pelican.py
node experiments/check-animation.cjs
```

真实服务验收脚本会创建、修改或删除页面，不能当作无副作用的常规测试。使用前阅读 [cmd/pagehub-verify/main.go](cmd/pagehub-verify/main.go) 和 [experiments/verify-patch.py](experiments/verify-patch.py)；后者绑定特定鹈鹕 UUID 和实验局域网地址，只适用于对应的本机环境。

开发代理的项目约束和验证要求见 [AGENTS.md](AGENTS.md)。

## 故障排查

| 现象 | 检查方式 |
| --- | --- |
| 本机也打不开 | 检查 `/_health`、端口是否被占用，以及 `~/.pagehub/pagehub.log` |
| 本机正常，手机打不开 | 确认同一局域网和正确 IPv4 地址，运行 `allow-lan.command` 检查防火墙许可 |
| MCP 401 | 客户端缺少或使用了错误令牌；服务启动后重新运行对应配置脚本 |
| MCP 403 | 管理请求来自非 loopback，或者 Host/Origin 不合法；客户端使用本机 URL |
| 新工具没有出现 | 重新打开客户端会话以刷新工具清单 |
| 补丁匹配失败 | 重新读取源码，检查空白和换行；重复文本增加上下文 |
| 版本冲突 | 重新读取页面，检查其他修改，再生成补丁 |

后台状态及手动重启：

```sh
launchctl print gui/$(id -u)/com.garyshu.pagehub
launchctl kickstart -k gui/$(id -u)/com.garyshu.pagehub
```

## 维护与反馈

由 [kuopenx](https://github.com/kuopenx) 维护。仓库有访问权限的用户可通过 [Issues](https://github.com/kuopenx/pagehub/issues) 反馈问题，附上程序版本、复现步骤和已脱敏的错误信息。提交行为变更时同步更新工具说明、相关测试和本文档。
