# Contributing

[English](CONTRIBUTING.md) · [简体中文](CONTRIBUTING.zh-CN.md)

Pagehub hosts self-contained HTML artifacts with one Go process and a token-authenticated MCP interface. Keep changes focused on that purpose.

## Development

Use Go 1.26.0+. From the repository root:

```sh
go build -o pagehub ./cmd/pagehub
go test -race ./...
go vet ./...
```

Use a separate data directory and port for development:

```sh
./pagehub serve --data-dir "$(mktemp -d)" --port 8766
```

Run real acceptance against a disposable instance:

```sh
go run ./cmd/pagehub-verify --binary ./pagehub
```

On macOS and Linux this installs a uniquely named temporary user service (LaunchAgent or systemd unit), exercises every CLI command and MCP tools, then uninstalls it. It never removes existing user pages. Linux acceptance requires systemd with an active user manager and its user bus; CI starts an isolated runner user manager before acceptance.

Go files must be formatted with `gofmt`. Tests should cover behavior, failure paths, idempotency and compatibility. Document interface changes in the README and tool descriptions. English is the default documentation language; update matching `.zh-CN.md` files together. See [AGENTS.md](AGENTS.md) for invariants and [docs/releasing.md](docs/releasing.md) for publishing.

## Pull requests

Explain the concrete problem, changed behavior and validation performed. Include relevant screenshots for visible Dashboard changes. Do not commit credentials, client configuration, real page data or generated binaries. Dependencies and data-format changes need a reason and compatibility plan. Publish source only; do not add precompiled downloads or signing workflows.

Contributions are licensed under the project's [MIT License](LICENSE). Report vulnerabilities through [SECURITY.md](SECURITY.md), rather than a public issue containing secrets or an exploit against someone's machine.
