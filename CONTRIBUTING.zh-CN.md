# 参与贡献

[English](CONTRIBUTING.md) · [简体中文](CONTRIBUTING.zh-CN.md)

Pagehub 使用一个 Go 进程和本机 MCP 托管自包含 HTML artifact。改动应围绕这个用途。

## 开发

使用 Go 1.26.0+，在仓库根目录执行：

```sh
go build -o pagehub ./cmd/pagehub
go test -race ./...
go vet ./...
```

开发时使用独立数据目录和端口：

```sh
./pagehub serve --data-dir "$(mktemp -d)" --port 8766
```

对一次性实例运行真实验收：

```sh
go run ./cmd/pagehub-verify --binary ./pagehub
```

macOS 和 Linux 验收安装一个名称唯一的临时用户服务（LaunchAgent 或 systemd unit），验证所有 CLI 命令与 MCP 工具后卸载；不删除既有用户页面。Linux 验收需要 systemd、已运行的用户管理器和用户 bus；CI 在验收前启动隔离 runner 的用户管理器。

Go 文件需要 `gofmt`。测试覆盖行为、失败路径、幂等性和兼容性。接口变化同步 README 和工具说明。英文是默认文档语言，修改时同步对应的 `.zh-CN.md` 文档。约束见 [AGENTS.zh-CN.md](AGENTS.zh-CN.md)，源码发布见 [docs/releasing.zh-CN.md](docs/releasing.zh-CN.md)。

## Pull request

说明具体问题、行为变化和实际执行的验证。Dashboard 的可见变化提供相关截图。不要提交凭据、客户端配置、真实页面数据或生成的可执行文件。依赖及数据格式变化需说明原因与兼容方案。只发布源码，不增加预编译下载或签名工作流。

贡献采用项目的 [MIT License](LICENSE)。漏洞通过 [SECURITY.zh-CN.md](SECURITY.zh-CN.md) 私密报告，不要在公开 Issue 中放置机密或针对他人机器的利用细节。
