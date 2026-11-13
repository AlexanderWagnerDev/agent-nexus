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

- AgentNexus is in alpha: there is no official version and no release process
  for contributors. Do not add version tags or releases.
- Keep changes focused and add tests for parser, policy or protocol changes.
- Run `gofmt`, `go vet ./...` and `go test ./...` before opening a PR.
- Update `docs/` when behavior or a design decision changes.
- Do not add telemetry, cloud dependencies or agent credential handling.
- Never weaken host policy checks to make a feature easier.
- Use synthetic data in tests, fixtures and screenshots. See
  [docs/PRIVACY.md](docs/PRIVACY.md).

## Security

Do not report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).

## Screenshots and UI fixtures

Screenshots use synthetic data only. The demo host speaks the real hub protocol
but serves fixtures from `cmd/nexus-demo`; it never reads a real agent store.

```bash
make build demo
node web/e2e/demo/seed.mjs      # starts a throwaway hub on :8790
node web/e2e/demo/shots.mjs     # writes docs/screenshots
cd web && npm run e2e:demo-ui   # DOM regression checks against the demo
cd web && E2E_SPAWN_DEMO=1 npm run e2e:visual
```

Login for the demo hub is `demo` / `demo-alpha-only`. The data directory lives
under the OS temp folder and is deleted on the next seed. Never point the demo
at a real hub or reuse real sessions. See [docs/PRIVACY.md](docs/PRIVACY.md).

### Browser executable

The Playwright scripts use the Chromium revision pinned by
`web/package-lock.json`. If that browser is not installed, point the scripts at
an existing Chromium/Chrome binary:

```bash
# Windows PowerShell
$env:PLAYWRIGHT_CHROMIUM_EXECUTABLE = "C:\Program Files\Google\Chrome\Application\chrome.exe"
# macOS / Linux
export PLAYWRIGHT_CHROMIUM_EXECUTABLE=/usr/bin/chromium
```

The value is passed to `chromium.launch({ executablePath })` by `smoke`,
`a11y`, `live`, `virtual`, `shots` and `visual`. `E2E_CHROME` remains an alias
for older local scripts. Do not edit a baseline because a different browser
renders fonts differently; refresh baselines only for intentional UI changes.

## License

By contributing, you agree that your contribution is licensed under the
project's MIT license.
