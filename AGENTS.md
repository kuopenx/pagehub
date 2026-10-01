# AGENTS.md

适用于整个仓库。用户文档见 [README.md](README.md)，接口以 `internal/server/tools.go` 的 Schema 与处理器为准，依赖以 `go.mod` 为准。

## 项目与代码

Pagehub 是单文件 HTML artifact 服务，一个 Go 二进制提供 CLI、LAN 页面、本机 MCP 与 macOS 后台管理。用户安装使用不依赖 Python 或 Node。

- `cmd/pagehub`：CLI 入口；`internal/cli`：命令、退出码、JSON 输出与诊断。
- `internal/server`：HTTP、MCP、嵌入 Dashboard、页面存储、修订号、补丁及日志。
- `internal/service`：用户级 launchd 安装、启停、旧标签迁移与升级回滚。
- `internal/clients`：Codex TOML、Claude JSON 配置；精确保留无关配置、拒绝覆盖其他 endpoint。
- `internal/config`：设置 schema、校验、私有原子写入；`internal/buildinfo`：程序版本与构建信息。
- `cmd/pagehub-verify`：使用隔离实例进行真实 CLI/MCP 验收；`examples`：自包含 HTML。
- `.github/workflows` / `.goreleaser.yaml`：CI、发布和可选 Apple 签名。

## 命令与验证

使用 Go 1.26.0+，从仓库根目录运行：

```sh
go build -trimpath -ldflags='-s -w' -o pagehub ./cmd/pagehub
go test -race ./...
go vet ./...
go run ./cmd/pagehub-verify --binary ./pagehub
```

Go 修改后运行 `gofmt`。HTTP 测试需要临时端口监听许可；环境不允许时明确报告，不能跳过后声称通过。真实验收默认使用临时数据、动态 UUID 和独立端口；macOS 使用唯一 LaunchAgent，验证后卸载。`--endpoint` 模式只能指向 loopback，验收页必须删除，既有页面必须保持不变。

仅文档修改核对链接、示例和当前接口，无需重启或重跑全套行为测试。发布前运行 `goreleaser check` 和 snapshot 构建，检查归档、校验和及安装。

## 必须保留的行为

- 一个进程服务全部页面；内存只索引元数据。保留单文件 UTF-8 HTML，不引入资源包、路径读取、URL 导入、业务配额、休眠或到期。
- 标题可重复，UUID 由服务器生成；update/patch 保持 UUID、URL 和创建时间。
- read 保留原始文本与换行，行号从 1 开始、首尾包含，支持超长行。
- patch 需要 expected_revision，非空 old_text 必须唯一匹配，包括重叠。顺序编辑，失败整批不发布、不修改元数据或版本。
- 新页和旧元数据从 revision=1 开始，每次成功 update/patch 增加一次；update 版本检查保持可选，不保存历史副本。
- 暂存完整文件和元数据再发布，保留中断恢复与并发一致性。修改格式要兼容既有 page.json，不能迁移或删除用户页面来整理代码。
- 页面和 Dashboard 允许 LAN；MCP 仅 loopback、合法 Host/Origin 与令牌。令牌首次生成后复用，不写死或打印。
- Dashboard 标题转义、刷新缓存一致性和有限日志轮转保持有效；日志不记录 HTML、认证头或令牌。
- 后台安装保持 macOS 用户级 LaunchAgent，不改成系统 daemon、root 服务或关闭防火墙。升级失败恢复旧二进制与配置，卸载默认保留数据。
- 所有 CLI 需有正常、失败和重复执行验证；客户端写入必须校验格式、检查并发变动并保留其他服务。

## 修改和提交

接口变化同步 typed 输入/输出、Schema、说明、annotations、Instructions 与文档；Schema 明确类型且拒绝额外字段。仅引入确有用途的依赖，锁定版本；依赖变更刷新第三方版权声明。

使用临时目录和测试令牌。管理凭据只发送至 loopback；LAN 测试不带凭据。修改嵌入 Dashboard 后需重新构建。

只在任务要求升级时替换实际安装；先通过构建与验证，升级后检查健康、既有内容/令牌/修订号持久化和 LAN。所有客户端共享一个服务。

提交前检查 diff、暂存文件和机密；不提交运行数据、用户配置、令牌、缓存、二进制或本机报告。发布标签、Release 和 tap 更新流程见 [docs/releasing.md](docs/releasing.md)。完成报告说明实际验证与限制，不把脚本检查当作视觉验收，不把未签名产物说成已公证。
