# 发布 Pagehub

[English](releasing.md) · [简体中文](releasing.zh-CN.md)

Pagehub 只分发源码，支持 Homebrew、`go install` 和本地源码构建。不要向 Release 上传预编译可执行文件、安装包、二进制归档或其校验和附件。发布流程不包含 Apple 签名和公证。

## 准备

维护者需要推送和 Release 权限。Homebrew tap 是 [kuopenx/homebrew-tap](https://github.com/kuopenx/homebrew-tap)，配方使用 Go 构建带版本标签的源码归档。Pagehub 没有自更新器。

## 验证与发布

1. 更新 `internal/buildinfo.Version`、README 版本及英文、简体中文文档。新增对应的 `docs/releases/vX.Y.Z.md` 和 `docs/releases/vX.Y.Z.zh-CN.md` 发布说明；源码发布工作流使用标签中的英文文件作为 Release 正文。
2. 执行 [CONTRIBUTING.zh-CN.md](../CONTRIBUTING.zh-CN.md) 中的检查与隔离实例真实验收。改动工作流时，同时检查 YAML 和内嵌 shell 语法。
3. 依赖变更时更新 `THIRD_PARTY_NOTICES.md`，检查 diff 中的机密和生成文件。
4. 推送提交，等待 macOS/Linux CI 成功。
5. 为通过检查的提交推送带注释的 `vX.Y.Z` 标签。**Source release** 工作流检查标签源码、运行真实验收，再创建带安装说明的草稿；不上传任何附件。也可指定已有标签手动运行。
6. 确认草稿的附件列表为空，工作流成功后发布。GitHub 自动提供源码 ZIP 和 tar.gz 链接。
7. 运行 tap 的 **Update Pagehub formula** 工作流，或等待每日计划执行。它读取最新公开源码 Release，更新标签 URL、源码 SHA256 和嵌入的提交号。验证源码安装及 `brew test kuopenx/tap/pagehub`。
8. 使用临时 `GOBIN` 验证 `go install github.com/kuopenx/pagehub/cmd/pagehub@vX.Y.Z`，检查安装版本。README 的 `@latest` 使用最高已发布 Go 模块版本标签，因此不要保留失败的发布标签。

不要移动已经公开的标签。程序版本、页面修订号和设置 schema 版本含义不同。源码归档没有可执行文件附件。本地构建报告源码版本，Homebrew 还嵌入标签提交号。

## 清理已有 Release

删除以前上传的二进制归档及对应的 `checksums.txt` 附件，包括草稿。保留源码版本标签，把公开发布说明改为源码安装。GitHub 自动提供的源码归档符合预期，不是二进制安装包。仓库不应提交可执行文件；没有提交过二进制时，不应仅为删除二进制而改写源码历史。

## 升级兼容性

从源码安装新版本后，macOS 或 Linux 使用 `pagehub setup` 更新托管的后台可执行文件，前台实例另行重启。Pagehub 不会覆盖 Homebrew 的可执行文件。Setup 保留页面、令牌、设置和修订号，等待健康检查，启动失败时恢复旧可执行文件及服务配置。旧 0.2.0 launchd 标签在成功启动后迁移为 `io.pagehub.agent`。

修改 `--service-name` 时，Setup 也会迁移保存的自定义服务标签。就绪检查要求健康响应来自 launchd 或 systemd 报告的 Pagehub PID；其他服务返回 HTTP 200 或重定向均不算通过。本地管理客户端固定 loopback 地址和端口，禁止重定向，也不使用环境代理。

设置目前使用 schema 版本 1，缺少修订号的旧页面元数据按 revision 1 读取。未来页面格式变化需要兼容性样本、重启验证和回滚策略，再发布。
