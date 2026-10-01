# Pagehub

[English](README.md) · [简体中文](README.zh-CN.md)

**Turn AI-generated, single-file HTML into pages you can open on your phone.**

One Go program, one background process, and one port serve a LAN dashboard, HTML pages, and a local MCP endpoint. Codex, Claude Code, and other MCP clients submit HTML or precise edits directly, without knowing where files are stored.

Use it for personal artifacts, SVG animations, interactive demos, and visualizations. Titles may repeat; server-generated UUIDs identify pages. There is no sleep, expiration, page-count quota, or application-level size quota. Current version: **0.3.2**. License: [MIT](LICENSE).

## Installation

Pagehub is distributed **only as source**. Use Homebrew, `go install`, or build from a checkout. GitHub Releases contain version notes and automatic source archives; no precompiled binaries or installers are published.

**macOS supports background installation; Linux supports foreground serving.** Python and Node.js are not required.

### Homebrew (macOS)

```sh
brew install kuopenx/tap/pagehub
pagehub setup
pagehub connect codex    # or: pagehub connect claude
pagehub doctor
pagehub open
```

Homebrew builds the versioned source and manages Go as a build dependency. `setup` installs a user LaunchAgent that starts when you log in, without root. Run it again to update an existing background instance.

### Go (macOS and Linux)

Requires Go 1.26.0 or newer:

```sh
go install github.com/kuopenx/pagehub/cmd/pagehub@latest
```

Make sure `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset, is on your `PATH`. On macOS, continue with `pagehub setup` and the client commands above. On Linux, run `pagehub serve`; use a second terminal to connect a client.

For building from a checkout, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Usage

The local dashboard is <http://127.0.0.1:8765/>. Connect your phone and computer to the same LAN and open `http://<computer-LAN-IPv4>:8765/`. `setup`, `doctor`, and `open` print the available LAN links.

Ask your MCP client to create a page, then open the returned URL or find it on the dashboard. Creating, editing, and deleting pages takes effect immediately. Refresh the browser to see changes.

| Command | Behavior |
| --- | --- |
| `pagehub serve` | Run HTTP and MCP in the foreground |
| `pagehub setup` | Install or upgrade the macOS background service, start it, and wait for health |
| `pagehub service start/stop/restart/status` | Manage the user background service |
| `pagehub connect codex/claude` | Register Pagehub while preserving other client configuration |
| `pagehub disconnect codex/claude` | Remove the Pagehub registration pointing to this service |
| `pagehub doctor` | Check the service, HTTP, authenticated MCP, client configuration, and LAN addresses |
| `pagehub open` | Open the dashboard; still print its URL when no browser is available |
| `pagehub version` | Print version, commit, and build date |
| `pagehub uninstall` | Remove the service and managed executable; keep pages, token, settings, and client configuration |

Every command supports `--help`. Use `--json` for machine-readable output. Exit codes are 0 for success, 1 for operation failure, and 2 for argument parsing or validation failure. Errors go to stderr. `--data-dir`, `--port`, and `--service-name` override saved settings; connection commands also accept `--config-file`.

The default listener is IPv4 `0.0.0.0:8765`. Separate instances need distinct ports, data directories, and service names. For a foreground development instance:

```sh
pagehub serve --port 8766 --data-dir "$(mktemp -d)"
```

### Connect clients

`connect` configures an Authorization header for the local HTTP endpoint without printing the token. It refuses to overwrite an MCP registration with the same name pointing elsewhere. Client configuration permissions are set to `0600`. Reopen existing client sessions if needed to discover the six tools.

For a custom data directory or port, use the same `--data-dir` for `setup` and `connect`; the endpoint uses the saved port. Default configuration paths are `~/.codex/config.toml` and `~/.claude.json`. Use `--config-file` if your client reads another file.

### Upgrade and uninstall

For Homebrew:

```sh
brew upgrade pagehub
pagehub setup
pagehub doctor
```

For Go installations, rerun `go install github.com/kuopenx/pagehub/cmd/pagehub@latest`, then run `pagehub setup` on macOS and `pagehub doctor`. Restart a foreground Linux instance to use the new executable.

`setup` copies the current executable into `~/.pagehub/bin/pagehub`; it does not overwrite package-manager files. If startup fails, it restores the previous executable, settings, and service registration. Existing pages, URLs, token, and revisions remain intact. Legacy launchd registrations migrate to `io.pagehub.agent`.

To remove the background service while keeping pages:

```sh
pagehub disconnect codex
pagehub disconnect claude
pagehub uninstall
brew uninstall pagehub   # if installed with Homebrew
```

Page data is never deleted automatically. The management token is randomly generated once and persists across restarts and upgrades.

## MCP interface

HTTP and MCP share one port. The management endpoint is `http://127.0.0.1:8765/_mcp`. Pagehub uses the official Go SDK with stateless Streamable HTTP; clients negotiate a protocol version accepted by that SDK.

| Tool | Arguments | Behavior |
| --- | --- | --- |
| `create_page` | `title, media_type, html` | Accept complete HTML, require `text/html`, and generate a UUID |
| `list_pages` | `query?, offset?, limit?` | Search titles/UUIDs, newest creation first; default limit 100, 0 means all |
| `read_page` | `id, start_line?, end_line?` | Return exact source and revision, with optional 1-based inclusive line bounds |
| `patch_page` | `id, expected_revision, edits` | Apply uniquely matching exact-text replacements; save only if the entire batch succeeds |
| `update_page` | `id, title?, media_type?, html?, expected_revision?` | Rename or replace content, with optional revision checking |
| `delete_page` | `id` | Delete managed files and the in-memory index entry; the URL then returns 404 |

See [MCP examples](docs/mcp.md) and the self-contained [pelican riding a bicycle](examples/pelican-bicycle.html). Revisions protect concurrent edits; historical copies are not retained.

## Content and access boundaries

Only complete, single-file UTF-8 HTML is hosted. Inline CSS, JavaScript, SVG, and data URLs are supported. Paths, URL imports, PDF, ZIP, and separate assets are not accepted. Browsers may still request external references; Pagehub does not download or manage them.

Pages and the dashboard are accessible over the LAN without login. MCP management requires loopback, a valid Host/Origin, and a Bearer token. HTML can execute JavaScript, and all pages currently share one origin. Use trusted content on a trusted LAN. See [SECURITY.md](SECURITY.md) for the trust boundary and private vulnerability reporting.

The in-memory index contains metadata only. Content is read on demand and temporarily occupies memory during operations. Disk and memory still impose practical limits. Logs rotate at approximately 1 MiB, with at most two files; they contain neither HTML nor authentication information.

## Storage and troubleshooting

The default `~/.pagehub` directory contains `settings.json`, `token`, logs, `bin/pagehub`, and `pages/<uuid>/{index.html,page.json}`. Pagehub manages these files; MCP callers do not need to access them.

Start with `pagehub doctor --json`:

| Problem | Check |
| --- | --- |
| Service is not running | `pagehub service status`, `pagehub service start`, and `pagehub.log` |
| Local access works but phone access fails | Same Wi-Fi, correct IPv4, and Pagehub's incoming firewall permission in macOS Settings |
| MCP returns 401 | Rerun `pagehub connect <client>` with the same data directory |
| MCP returns 403 | Use the local management URL and check Host/Origin; LAN URLs are for pages |
| New tools are missing | Reopen the client session |
| Patch match or revision conflict | Read the page again and check context and the latest revision |

## Maintenance

See [AGENTS.md](AGENTS.md) for development rules and [docs/releasing.md](docs/releasing.md) for source releases. Maintained by [kuopenx](https://github.com/kuopenx). Report problems and suggestions through [Issues](https://github.com/kuopenx/pagehub/issues), with version, reproduction steps, and redacted diagnostics.

Dependency notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
