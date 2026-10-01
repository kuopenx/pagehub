# AGENTS.md

本文件适用于整个仓库，提供开发代理所需的项目约束和执行指引。用户文档见 [README.md](README.md)；实际接口以 `tools.go` 的 Schema 和处理器为准，依赖以 `go.mod` 为准。

## 项目与代码位置

Pagehub 是面向自用 HTML artifact 的 Go 服务，一个进程、一个端口同时提供 LAN 页面和本机 MCP。核心服务使用标准库与官方 MCP Go SDK，没有前端构建链。

- `main.go`：启动、参数、固定持久化令牌、日志轮转、优雅退出。
- `store.go`：页面文件和元数据、内存索引、读取、修订号、写入及中断恢复。
- `tools.go`：六个 MCP 工具的输入/输出、Schema、说明和 annotations。
- `http.go` / `dashboard.html`：HTTP 路由、公开 Dashboard、缓存和管理访问限制；Dashboard 使用 `go:embed`。
- `store_test.go` / `patch_test.go` / `http_test.go`：存储、原子补丁、并发冲突、真实 HTTP MCP 和访问隔离测试。
- `install-macos.py` / `configure-*.py` / `allow-lan.command`：安装与本机集成脚本。
- `cmd/pagehub-verify/` / `experiments/`：会访问真实服务的验收工具及示例。

## 开发命令

从仓库根目录运行。需要 Go 1.26.0+；配置 Codex 的 Python 脚本需要 Python 3.11+。

```sh
go build -trimpath -ldflags='-s -w' -o pagehub .
go test -race ./...
go vet ./...
```

开发服务器与已安装服务分开存储并使用不同端口：

```sh
./pagehub --port 8766 --data-dir "$(mktemp -d)"
```

该进程仍监听 LAN IPv4；退出后临时目录不会自动删除。测试失败若是沙箱禁止绑定本机端口，区分环境错误与代码错误，并在环境允许时重新执行；不能跳过后声称通过。

## 必须保留的行为

- 一个 Go 进程服务所有页面；持久存储默认位于 `~/.pagehub`。只在内存保留元数据，不缓存所有页面正文，也不增加每页工作进程。
- 页面保持单文件 UTF-8 HTML。内嵌 CSS、JS、SVG、data URL 可用；不扩展成资源包、路径读取或 URL 导入。没有数量、业务大小配额、休眠或过期策略。
- UUID 是服务器生成的标识；标题可重复。更新和补丁保留 UUID、URL、创建时间。
- `read_page` 保留原始文本和换行，行号从 1 开始且首尾包含；避免使用有默认行长限制的 Scanner 截断长行。
- `patch_page` 要求 `expected_revision`；旧文本必须非空且唯一匹配，包括重叠匹配。编辑顺序执行，失败整批不发布，不修改元数据或版本号。
- 新页面和无修订号的旧元数据从 1 开始；成功 update/patch 增加一次并持久化。修订号不是历史版本存储。`update_page` 的版本检查保持可选。
- HTML 和元数据使用暂存发布及备份恢复；文件打开与元数据读取须对应同一代内容。并发修改在同一锁内检查版本与提交。
- HTTP 页面和 Dashboard 对 LAN 开放；MCP 只允许 loopback、合法 Host/Origin 及 Bearer token。令牌首次生成后复用，不写死在源码中。
- 保留刷新后的缓存一致性、Dashboard 标题转义、有限日志轮转；日志不记录 HTML、令牌或认证头。

## 修改约定

- Go 文件修改后运行 `gofmt`；沿用现有标准库、错误处理和 typed MCP handler，避免无必要的依赖或抽象。
- 修改接口时同步 Go 输入/输出结构、JSON Schema、工具说明、annotations、服务器 Instructions 和 README。Schema 保持明确的资源类型、参数校验及 `additionalProperties: false`。
- 存储格式变更需兼容已有 `page.json`，验证重启和恢复行为。不要为了整理代码迁移或删除用户的真实页面。
- 变更影响嵌入的 Dashboard 时需要重新构建，单独编辑磁盘 HTML 不会更新已运行的二进制。
- 安装脚本仍使用用户级 LaunchAgent，在登录后启动；不要改成系统 LaunchDaemon 或关闭 macOS 防火墙。
- 令牌、客户端配置、页面运行数据、编译产物、缓存和本机验收报告不得提交；保持 `.gitignore` 有效，输出认证配置时脱敏。

## 验证与完成标准

- Go 或 HTTP/MCP 行为变更：运行 `go test -race ./...`、`go vet ./...` 和构建；为受影响的边界行为补充有价值的测试。
- 仅文档变更：检查本地链接、命令、参数和示例是否符合实现；无需重启服务或重复全套行为测试。
- 动画示例变更：需要 Python 3 和 Node.js，运行 `python3 experiments/prepare-pelican.py`、`node experiments/check-animation.cjs`。这验证脚本逻辑，不等于浏览器视觉验收。
- 普通测试使用 `t.TempDir`、临时端口及测试令牌。不要把 `cmd/pagehub-verify` 或 `experiments/verify-patch.py` 当作常规测试：它们会创建、修改、删除真实页面；后者还绑定特定 UUID 和 LAN 地址。
- 若任务要求真实 MCP 验证，用 MCP 工具完成页面操作。管理凭据仅发送至 loopback；验证 LAN 页面时不带令牌。检查页面内容、URL、修订号和其他页面是否保持预期。
- 若任务要求安装升级，先完成构建与验证，再更新已安装二进制；检查后台健康、内容持久化和 LAN 访问。客户端配置不应引入第二个服务进程。
- 提交前检查 diff 和暂存文件；提交信息说明具体变更。完成报告写明修改、验证结果和尚未解决的问题，不能把脚本检查写成视觉验证或把未执行的检查写成通过。

本文应保持简短、可执行、与当前实现一致；新发现的长期项目约束可补充到对应段落，不记录会话流水或本机令牌、IP、页面 ID。
