# 安全

[English](SECURITY.md) · [简体中文](SECURITY.zh-CN.md)

## 支持版本

安全修复应用于最新发布的 0.x 版本。报告问题前，请先从旧版本升级。

## 报告

通过仓库 Security 页的 **Report a vulnerability** 私密报告。包含受影响版本、复现步骤、预期影响和最小示例。不要包含真实令牌、私密页面内容或他人的个人信息。若私密报告不可用，可公开发起仅请求私密联系方式的 Issue，不包含利用细节。

## 部署边界

Pagehub 用于可信局域网上的可信 HTML。公开页面和 Dashboard 没有登录。提交的 JavaScript 可以执行，所有页面目前共享同一 origin；不可信页面可能读取该 origin 上的其他公开页面。Pagehub 不承诺租户或内容隔离。

MCP 管理要求持久化 Bearer token、loopback 请求及合法 Host/Origin。不要把令牌发送到 LAN 地址，不要用公开代理暴露管理 endpoint，也不要把令牌放进 HTML artifact。客户端配置和数据目录属于本机私密文件。

后台安装使用 macOS 用户级 LaunchAgent，不关闭防火墙、不进行系统级安装，也不以 root 运行服务。Pagehub 只分发源码，Homebrew 和 Go 安装在本机构建可执行文件；不发布预编译二进制或安装包。源码安装不提供 Apple Developer ID 签名或公证声明。
