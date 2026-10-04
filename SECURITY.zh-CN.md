# 安全

[English](SECURITY.md) · [简体中文](SECURITY.zh-CN.md)

## 支持版本

安全修复应用于最新发布的 0.x 版本。报告问题前，请先从旧版本升级。

## 报告

通过仓库 Security 页的 **Report a vulnerability** 私密报告。包含受影响版本、复现步骤、预期影响和最小示例。不要包含真实令牌、私密页面内容或他人的个人信息。若私密报告不可用，可公开发起仅请求私密联系方式的 Issue，不包含利用细节。

## 部署边界

Pagehub 用于可信局域网上的可信 HTML。公开页面和 Dashboard 没有登录。提交的 JavaScript 可以执行，所有页面目前共享同一 origin；不可信页面可能读取该 origin 上的其他公开页面。Pagehub 不承诺租户或内容隔离。

MCP 管理只校验请求中的 Bearer token 是否在本机私密令牌列表或旧令牌文件中有效。本机与远程客户端持有令牌即可使用全部工具，包括更新和删除；不限制来源 IP、Host 或 Origin。用于自己的可信局域网。HTTP 不加密令牌；公网部署需要 HTTPS 等加密传输。令牌只通过目标 MCP endpoint 的 Authorization 头发送，不放入页面 URL、查询参数或 HTML artifact。客户端配置和数据目录属于本机私密文件。只有显式本地 CLI 命令 token generate、token show 和 token rotate 会输出令牌；Dashboard、MCP 工具、日志和常规命令输出不包含令牌。设备令牌彼此独立，权限相同，共享同一个页面存储；名称只是标签，不是经过认证的设备身份。新增设备令牌不改写原默认令牌。更换或废弃其中一个令牌后，其他令牌继续有效，旧令牌的后续请求立即被拒绝，无需重启服务；已经通过认证的进行中操作可能完成。废弃状态在 Setup、重启和升级后保持，直到显式生成或更换令牌。

Pagehub 的 doctor 和验收客户端只向配置的数字 loopback 地址、端口及 MCP 路径发送凭据，禁用 HTTP 重定向和环境代理。

后台安装使用 macOS 用户级 LaunchAgent，不关闭防火墙、不进行系统级安装，也不以 root 运行服务。Pagehub 只分发源码，Homebrew 和 Go 安装在本机构建可执行文件；不发布预编译二进制或安装包。源码安装不提供 Apple Developer ID 签名或公证声明。
