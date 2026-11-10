# Contributing

Thanks for your interest in AgentNexus.

## Development setup

- Go 1.27+
- Node.js 22+ (only for `node --check`; the browser client has no build step)

```bash
make test
make lint
make build
```

Run the hub with a disposable data directory:

```bash
go run ./cmd/nexus-hub --addr 127.0.0.1:8080 --data-dir ./.nexus
```

## Pull requests

- Keep changes focused and add tests for parser, policy or protocol changes.
- Run `gofmt`, `go vet ./...` and `go test ./...` before opening a PR.
- Update `docs/` when behavior or a design decision changes.
- Do not add telemetry, cloud dependencies or agent credential handling.
- Never weaken host policy checks to make a feature easier.

## Security

Do not report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contribution is licensed under the
project's MIT license.
