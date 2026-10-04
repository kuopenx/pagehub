# AGENTS.md

[English](AGENTS.md) · [简体中文](AGENTS.zh-CN.md)

These instructions apply throughout the repository. User documentation is in [README.md](README.md). The schemas and handlers in `internal/server/tools.go` define the interface; `go.mod` defines dependencies.

## Project and code

Pagehub hosts single-file HTML artifacts. One Go executable provides the CLI, LAN pages, token-authenticated MCP, and macOS/Linux background management. Installation and use do not depend on Python or Node.

- `cmd/pagehub`: entry point; `internal/cli`: commands, exit codes, JSON output, and diagnostics.
- `internal/server`: HTTP, MCP, embedded dashboard, storage, revisions, patches, and logging.
- `internal/service`: user launchd/systemd installation, lifecycle, legacy migration, and upgrade rollback.
- `internal/clients`: Codex TOML and Claude JSON; preserve unrelated configuration and reject foreign endpoints.
- `internal/localhttp`: pin management requests to a numeric loopback authority; disable redirects and environment proxies.
- `internal/config`: settings schema, validation, and private atomic writes; `internal/buildinfo`: version and build information.
- `cmd/pagehub-verify`: real CLI/MCP acceptance using an isolated instance; `examples`: self-contained HTML.
- `.github/workflows`: CI and source-only Releases. Do not publish precompiled executables or add signing/notarization workflows.

## Commands and validation

Use Go 1.26.0+ from the repository root:

```sh
go build -trimpath -ldflags='-s -w' -o pagehub ./cmd/pagehub
go test -race ./...
go vet ./...
go run ./cmd/pagehub-verify --binary ./pagehub
```

Run `gofmt` after Go changes. HTTP tests need temporary listening ports; report environmental restrictions rather than claiming skipped tests passed. Acceptance uses temporary data, dynamic UUIDs, and an independent port. On macOS/Linux it uses a unique user LaunchAgent/systemd unit and uninstalls it afterward. Linux requires an active systemd user manager. `--endpoint` may target only loopback: delete acceptance pages and leave existing pages unchanged.

For documentation-only changes, check links, examples, and the current interface; there is no need to restart the service or rerun every behavior test. For workflow changes, validate YAML and shell syntax. Before a source release, require successful CI and real acceptance; verify zero uploaded binary assets and a working source installation.

## Required behavior

- One process serves all pages; only metadata is indexed in memory. Keep single-file UTF-8 HTML. Do not add asset bundles, file-path reads, URL imports, business quotas, sleep, or expiration.
- Titles may repeat; the server generates UUIDs. Updates and patches preserve UUID, URL, and creation time.
- Reads preserve exact text and line endings. Line bounds start at 1 and are inclusive; support very long lines.
- Patches require `expected_revision`. Nonempty `old_text` must match uniquely, including overlaps. Apply edits sequentially; any failure publishes nothing and changes neither metadata nor revision.
- MCP create requires `created_by`; update/patch require `updated_by`, as caller-reported `model-name / reasoning-effort`. Creation records only the creator; `updated_by` is absent until a successful update/patch (revision 2+); preserve the creator on later writes. Failed writes change neither attribution nor revision. Legacy missing fields stay unknown; never infer a model.
- New pages and legacy metadata start at revision 1. Successful updates/patches increment once. Update revision checking is optional; do not keep historical copies.
- Stage complete content and metadata before publishing. Preserve interrupted-write recovery and concurrency consistency. Remain compatible with existing `page.json`; never delete or migrate user pages merely to reorganize code.
- Pages and dashboard allow LAN access. MCP accepts local and remote requests with any active Bearer token from the local token registry or the legacy token file; do not restrict source IP, Host, or Origin. Preserve the original single token as default; named device tokens coexist and can be rotated/revoked independently without affecting others. Reuse each token until explicit rotation/revocation. Never hard-code it; only explicit token generate/show/rotate commands may print it for copying.
- Preserve escaped dashboard titles, refresh/cache consistency, and bounded log rotation. Logs must not contain HTML, Authorization headers, or tokens.
- Keep the macOS user LaunchAgent and Linux user systemd unit. Do not replace it with a system daemon, root service, or disabled firewall. Failed upgrades restore the previous executable and configuration; uninstall keeps data by default.
- Verify CLI success, failure, and repeated execution. Validate client configuration, detect concurrent changes, and preserve unrelated services.

## Changes and commits

For interface changes, update typed input/output, schemas, descriptions, annotations, instructions, and documentation together. Schemas must specify types and reject extra fields. Add dependencies only for a concrete purpose, pin versions, and refresh dependency notices when needed.

Keep English as the default documentation language and update the corresponding `.zh-CN.md` documents in the same change. Legal license texts and dependency license notices retain their authoritative originals.

Use temporary directories and test tokens. Keep local management clients pinned to loopback. LAN MCP tests use isolated instances and temporary test tokens; LAN page checks carry no credentials. Rebuild after changing the embedded dashboard.

Replace the user's installed service only when the task calls for an upgrade. Build and validate first; afterward check health, existing content/token/revision persistence, and LAN access. All clients share one service.

Before committing, review the diff, staged files, and secrets. Do not commit runtime data, user configuration, tokens, caches, binaries, or local reports. Version tags, Releases, and tap maintenance are described in [docs/releasing.md](docs/releasing.md). Reports must distinguish actual checks from visual acceptance and must not claim signing or notarization that was not performed.
