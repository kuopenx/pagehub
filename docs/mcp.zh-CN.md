# MCP 用法

[English](mcp.md) · [简体中文](mcp.zh-CN.md)

通过兼容 MCP 的客户端连接本机 Pagehub 服务。工具参数直接包含 HTML 文本，不是文件路径。标题可重复，所有操作使用服务器生成的 UUID。

## 创建

`create_page` 参数：

```json
{"title":"Hello","media_type":"text/html","html":"<!doctype html><html><meta charset=\"UTF-8\"><h1>Hello</h1></html>"}
```

返回的 `page` 包含 ID、标题、时间戳、字节数、修订号、路径、本机 URL 和 LAN URL。手机使用 Wi-Fi LAN URL。Create/update/patch 也返回 Dashboard URL。

## 读取与补丁

`read_page` 参数：

```json
{"id":"<returned UUID>"}
```

返回精确的 `content`、`page.revision`、`start_line`、`end_line` 和 `total_lines`。可选行号从 1 开始，首尾包含，保留原始换行，不插入行号。结束行超过文件末尾时截断，开始行超过末尾时报错。省略行号则读取整个文件。

将返回的修订号用于 `patch_page`：

```json
{"id":"<returned UUID>","expected_revision":1,"edits":[{"old_text":"<h1>Hello</h1>","new_text":"<h1>Updated</h1>"}]}
```

每个非空 `old_text` 必须恰好匹配一次，重叠匹配也计入。文本重复时添加上下文。编辑按顺序执行，后续编辑可以看到前面的结果。空 `new_text` 删除原文本。不支持正则或模糊匹配。

修订号过期、任意匹配失败或最终 HTML 无效时，不保存任何变化。冲突后重新读取并评估补丁。成功时修订号只增加一次，并保留 ID、URL 和创建时间。

新页面及缺少修订号的旧元数据从 1 开始。`update_page` 也增加修订号；它的 `expected_revision` 可选，省略表示无条件更新。修订号不保留历史副本。

## 列表与删除

`list_pages` 支持不区分大小写的标题/UUID 搜索及 offset/limit 分页。默认 limit 为 100，0 表示全部。分页不限制存储页面总量。

`delete_page` 永久删除托管页面和元数据，该 URL 返回 404。它不删除调用者的原始文件，也不能撤回已经发送到浏览器的内容。

## 资源约束

只接受包含 `<html>` 元素的完整、非空 UTF-8 HTML 文本。支持内嵌 CSS、JavaScript、SVG 和 data URL，不接受资源包、文件路径、URL 导入、PDF、Markdown 或 ZIP。输入 schema 拒绝额外属性和不支持的媒体类型。

信任边界见 [SECURITY.zh-CN.md](../SECURITY.zh-CN.md)。认证凭据只用于 loopback 管理请求，不用于 LAN 页面 URL。
