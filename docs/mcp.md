# MCP usage

[English](mcp.md) · [简体中文](mcp.zh-CN.md)

Connect to Pagehub through a compatible MCP client on the same computer or LAN. Tool arguments contain actual HTML text, not filesystem paths. Titles may repeat; all operations use the server-generated UUID.

## Connection

Use `http://127.0.0.1:8765/_mcp` on the Pagehub host, or `http://<server-LAN-IPv4>:8765/_mcp` from another device on the same LAN. Configure the MCP client's HTTP headers:

```json
{"Authorization":"Bearer <server-token>"}
```

The original token is stored in `<data-dir>/token` (by default `~/.pagehub/token`) and remains valid as `default`. Additional device tokens are stored in private `<data-dir>/tokens.json`. Run `pagehub token generate --name phone` on the server to create a device token, or `pagehub token show --name phone` to copy its current value privately into that client configuration. Only explicit token generate/show/rotate commands output credentials. All active tokens have access to all six tools; each device may use a different token. Missing or incorrect tokens return HTTP 401. No source-IP, Host, or Origin allowlist is required.

`pagehub connect` continues to configure clients on the Pagehub host; configure remote clients manually. On remote clients, use a returned `page.lan_urls` link to open a page; `page.url` and `dashboard_url` use loopback and work only on the Pagehub host. Browser applications calling MCP across origins still need browser CORS handling; this change does not add CORS headers.

See [token maintenance](../README.md#manage-mcp-tokens) for generation, display, rotation, and revocation. Use `token list` for names and states without secrets. Rotation or revocation affects only clients using that selected token; other tokens continue working. Omit `--name` to manage the original default token.

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

Create records only `created_by`; `updated_by` is omitted from metadata and responses until the first successful update/patch (revision 2+). Revision-1 records from older versions also omit the updater when read, without rewriting their saved metadata. Successful updates/patches change only `updated_by`, atomically with content and revision. Failed operations change nothing. Read/list responses include both fields. Unknown legacy creators return an empty `created_by`; unknown updaters omit `updated_by`. Missing fields are hidden on the Dashboard; later updates preserve the unknown original creator.

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

See [SECURITY.md](../SECURITY.md) for the trust model. Send authentication credentials only in the Authorization header to the intended MCP endpoint, never in page URLs or HTML.
