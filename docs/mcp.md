# MCP usage

[English](mcp.md) · [简体中文](mcp.zh-CN.md)

Connect to the local Pagehub service through a compatible MCP client. Tool arguments contain actual HTML text, not filesystem paths. Titles may repeat; all operations use the server-generated UUID.

## Create

Arguments to `create_page`:

```json
{"created_by":"model-name / high","title":"Hello","media_type":"text/html","html":"<!doctype html><html><meta charset=\"UTF-8\"><h1>Hello</h1></html>"}
```

The returned `page` includes ID, title, timestamps, created_by/updated_by, byte size, revision, path, local URL and LAN URLs. Select the Wi-Fi LAN URL for a phone. Create/update/patch also return the dashboard URL.

## Read and patch

Arguments to `read_page`:

```json
{"id":"<returned UUID>"}
```

Read returns exact `content`, `page.revision`, `start_line`, `end_line`, and `total_lines`. Optional inclusive line bounds are 1-based, preserve original line endings and never insert line numbers. End beyond EOF is clamped; start beyond EOF is rejected. Omitted bounds read the whole file.

Use the returned revision in `patch_page`:

```json
{"id":"<returned UUID>","updated_by":"model-name / medium","expected_revision":1,"edits":[{"old_text":"<h1>Hello</h1>","new_text":"<h1>Updated</h1>"}]}
```

Every nonempty `old_text` must match exactly once, including overlapping occurrences. Add context when text is repeated. Edits run sequentially; later edits see earlier results. Empty `new_text` deletes the old text. There is no regex or fuzzy matching.

A stale revision, any failed match, or invalid final HTML saves nothing. Read again and reassess the patch after a conflict. Success increments the revision once and preserves ID, URL and creation time.

New pages and legacy metadata without a revision start at 1. `update_page` also increments the revision; `expected_revision` is optional for that tool, and omission means unconditional update. Revisions do not retain historical copies.

## Required model attribution

Every create must supply `created_by`; every update/patch must supply `updated_by`. Use the current model name and reasoning effort in the single-line format `model-name / high`. The examples contain placeholders, not a model to copy. Use `unknown` for an unavailable component (for example `model-name / unknown`), never invent it. Missing, blank, malformed, control-character, or non-string values are rejected. Attribution is caller-reported and publicly visible on the Dashboard; Pagehub cannot verify the running model.

Create sets both fields to the creator. Successful updates/patches change only `updated_by`, atomically with content and revision. Failed operations change nothing. Read/list responses include both fields. Legacy records use empty strings for unknown attribution, and are hidden on the Dashboard; later updates preserve the unknown original creator.

Example title-only `update_page`:

```json
{"id":"<returned UUID>","title":"Renamed","expected_revision":2,"updated_by":"model-name / low"}
```

Existing clients must send these new required parameters; reconnect or refresh tools to load the updated schemas and instructions.

## List and delete

`list_pages` supports a case-insensitive title/UUID query and offset/limit pagination. Default limit is 100; 0 means all. Pagination does not limit stored pages.

`delete_page` permanently removes the managed page and metadata; the URL returns 404. It does not delete the caller's original file or recall content already delivered to a browser.

## Resource contract

Only complete nonempty UTF-8 HTML text containing an `<html>` element is accepted. Inline CSS, JavaScript, SVG and data URLs are supported. No asset bundles, filesystem paths, imported URLs, PDF, Markdown or ZIP. Input schemas reject additional properties and unsupported media types.

See [SECURITY.md](../SECURITY.md) for the trust model. Authentication credentials are used only for loopback management requests, never LAN page URLs.
