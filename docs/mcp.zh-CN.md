# MCP 用法

[English](mcp.md) · [简体中文](mcp.zh-CN.md)

通过兼容 MCP 的客户端连接本机 Pagehub 服务。工具参数直接包含 HTML 文本，不是文件路径。标题可重复，所有操作使用服务器生成的 UUID。

## 创建

`create_page` 参数：

```json
{"created_by":"model-name / high","title":"Hello","media_type":"text/html","html":"<!doctype html><html><meta charset=\"UTF-8\"><h1>Hello</h1></html>"}
```

返回的 `page` 包含 ID、标题、时间戳、created_by/updated_by、字节数、修订号、路径、本机 URL 和 LAN URL。手机使用 Wi-Fi LAN URL。Create/update/patch 也返回 Dashboard URL。

## 读取与补丁

`read_page` 参数：

```json
{"id":"<returned UUID>"}
```

返回精确的 `content`、`page.revision`、`start_line`、`end_line` 和 `total_lines`。可选行号从 1 开始，首尾包含，保留原始换行，不插入行号。结束行超过文件末尾时截断，开始行超过末尾时报错。省略行号则读取整个文件。

将返回的修订号用于 `patch_page`：

```json
{"id":"<returned UUID>","updated_by":"model-name / medium","expected_revision":1,"edits":[{"old_text":"<h1>Hello</h1>","new_text":"<h1>Updated</h1>"}]}
```

每个非空 `old_text` 必须恰好匹配一次，重叠匹配也计入。文本重复时添加上下文。编辑按顺序执行，后续编辑可以看到前面的结果。空 `new_text` 删除原文本。不支持正则或模糊匹配。

修订号过期、任意匹配失败或最终 HTML 无效时，不保存任何变化。冲突后重新读取并评估补丁。成功时修订号只增加一次，并保留 ID、URL 和创建时间。

新页面及缺少修订号的旧元数据从 1 开始。`update_page` 也增加修订号；它的 `expected_revision` 可选，省略表示无条件更新。修订号不保留历史副本。

## 必填模型归属

每次创建必须提供 `created_by`；每次更新或 patch 必须提供 `updated_by`。按单行 `model-name / high` 格式填写当前模型名称和推理强度。示例中的模型是占位符，不要直接照抄。无法确定的部分使用 `unknown`（例如 `model-name / unknown`），不要编造。缺失、空白、格式错误、含控制字符或非字符串的值会被拒绝。归属是调用者自报的信息，公开显示在 Dashboard 上；Pagehub 无法核验实际运行的模型。

创建时两项均初始化为创建者。更新或 patch 成功时仅修改 `updated_by`，与内容和修订号一起原子保存；失败则不改变任何记录。Read/list 均返回这两项。旧元数据的未知归属返回空字符串，Dashboard 隐藏未记录的字段；后续更新仍保留未知的原创建者。

仅修改标题的 `update_page` 示例：

```json
{"id":"<returned UUID>","title":"Renamed","expected_revision":2,"updated_by":"model-name / low"}
```

现有客户端须发送这些新增的必填参数；重新连接或刷新工具，以加载更新后的 schema 和说明。

## 列表与删除

`list_pages` 支持不区分大小写的标题/UUID 搜索及 offset/limit 分页。默认 limit 为 100，0 表示全部。分页不限制存储页面总量。

`delete_page` 永久删除托管页面和元数据，该 URL 返回 404。它不删除调用者的原始文件，也不能撤回已经发送到浏览器的内容。

## 资源约束

只接受包含 `<html>` 元素的完整、非空 UTF-8 HTML 文本。支持内嵌 CSS、JavaScript、SVG 和 data URL，不接受资源包、文件路径、URL 导入、PDF、Markdown 或 ZIP。输入 schema 拒绝额外属性和不支持的媒体类型。

信任边界见 [SECURITY.zh-CN.md](../SECURITY.zh-CN.md)。认证凭据只用于 loopback 管理请求，不用于 LAN 页面 URL。
