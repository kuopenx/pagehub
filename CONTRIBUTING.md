# Contributing

Pagehub hosts self-contained HTML artifacts with one Go process and a local MCP interface. Keep changes focused on that purpose.

## Development

Use Go 1.26+. From the repository root:

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

On macOS this installs a uniquely named temporary LaunchAgent and exercises every CLI command before uninstalling it. It never removes existing user pages. On Linux it checks the foreground service and MCP tools; background management remains macOS-only.

Go files must be formatted with `gofmt`. Tests should cover behavior, failure paths, idempotency and compatibility. Document interface changes in the README and tool descriptions. See [AGENTS.md](AGENTS.md) for invariants and [docs/releasing.md](docs/releasing.md) for publishing.

## Pull requests

Explain the concrete problem, changed behavior and validation performed. Include relevant screenshots for visible Dashboard changes. Do not commit credentials, client configuration, real page data or generated binaries. Dependencies and data-format changes need a reason and compatibility plan.

Contributions are licensed under the project's [MIT License](LICENSE). Report vulnerabilities through [SECURITY.md](SECURITY.md), rather than a public issue containing secrets or an exploit against someone's machine.
