# Verification status

Last updated: 2026-11-13. This file records what was actually executed in the
development environment and what remains a manual step. It is deliberately
conservative.

## Automated and executed here

| Area | Evidence |
| --- | --- |
| Go unit/integration | `gofmt -l .`, `go vet ./...`, `go test ./...` pass locally (Windows); CI runs Linux, macOS and Windows plus `go test -race` |
| Static analysis | `staticcheck` passes; `govulncheck` reports no vulnerabilities |
| Browser unit tests | `node web/scripts/check-syntax.mjs`, `npm test` pass |
| Browser smoke | `npm run e2e` passes, including bell badge geometry, focus return for palette/notifications and the five-entry mobile bar with a More menu |
| Browser UI regressions | `npm run e2e:demo-ui` (with `E2E_SPAWN_DEMO=1`) covers tab dedupe, cross-host project grouping, the bell badge, host overflow actions, the Git diffstat list and the mobile sticky header |
| Virtualized 10k sessions | `node e2e/virtual.mjs`: ~20 rows initially, 33 after 1,400 sessions, 222/252 DOM nodes, no duplicates |
| Accessibility | axe-core scan passes desktop (dark and light) and mobile for home, projects, sessions, usage, fan-out, runs, hosts, settings and a structured chat; no critical/serious violations |
| Visual regression | Four stable demo views compare at 0 px; activity views are excluded because elapsed times are live |
| Load/soak harness | `npm run e2e:soak` starts up to 12 synthetic hosts, opens structured runs and samples goroutines, heap and FDs from `/metrics`; a 0.25 min smoke run (2 hosts, 3 runs, 8 samples) showed goroutines 14→16 and no FD growth. The 72 h run still has to be executed on a dedicated Linux host |
| Device signing | Ed25519 and P-256/P1363 signature tests, unsigned-write rejection, per-device nonce replay rejection, step-up key registration and the stop/prompt/approval/input paths |
| Multi-host simulation | Three fake hosts, six sessions, three parallel structured runs, host-disconnect isolation and hub-restart metadata persistence |
| Installer/update | Active-run refusal, stale runtime snapshot, checksum and version checks, rollback path, fake-host tests |
| Prompt queue | Store/API tests for order, schedule, pause, atomic claim, lease mismatch, crash recovery, duplicate prevention |
| Tasks | API tests for creation, ownership isolation, dependency blocking, transitive cycle rejection, readiness propagation, delegation prompt delivery, run/approval lifecycle sync and hub-restart reconciliation |
| Host health/detail | Unit tests for every health branch and an API test for health, recent sessions, audit access and favorites |
| Host doctor | Host-side safe-report test (no paths/keys) and hub API test with a scripted host answering `doctor` |
| Diagnostics | Admin payload and redacted export tests; non-admin denial; no secret-like fields in the export |
| Run limits | Per-host/user/project limit rejection returns HTTP 429 `run_limit` before a host command is created |
| Run error codes | Classifier table test and persistence test for failed runs |
| Devices | Bulk revoke-others test keeps the current device and revokes another session |
| Session relations/usage | Parent/forks/children API and reported usage aggregation tests |
| Fan-out diffs | Host stat-only git mode test and browser fan-out rendering; changed-file comparison uses host-reported status only |
| MCP | Auth, revocation, initialize, tools/list, tool calls and error results |
| CLI | Real local hub login, `sessions` and `logout`; build passed for Windows locally and all targets in CI |
| OpenCode 2.2.7 protocol | Read-only SQLite schema and `/openapi.json` inspected on a local 2.2.7 install; native session create/interrupt and fork contract exercised without a paid model turn; daily driver tests use a fake HTTP server |

## Not verified here

- Three real Windows hosts through Portainer, a real browser over the public
  internet, a real host disconnect and a real hub process restart.
- Real Claude Code and Gemini CLI sessions. Neither CLI is installed.
- Real OpenCode 2.2.7 structured prompt execution. The local 2.2.7 install was
  inspected read-only (SQLite schema, `/openapi.json`, session create and
  interrupt endpoints); a real model turn was not started because it would
  incur provider cost. Structured fork is implemented and fake-server tested;
  OpenCode rejects forks of empty sessions, so a fork requires a session with
  at least one turn.
- Real Codex 0.156.1 prompt execution through `app-server`; the installed CLI
  version is recorded, but only the generated schema and a fake app-server are
  covered.
- Real ntfy/Gotify/Telegram/Discord/Slack or generic webhook endpoints.
- Real MCP clients (Claude Desktop, Cursor) and real microphone dictation.
- PostgreSQL, large-scale load/soak (50 hosts/200 viewers/72 h), external
  penetration test and manual WCAG 2.2 AA audit.
- Container/Portainer runtime in this environment.

## Manual steps for the operator

1. Deploy the hub behind HTTPS/WebSocket and pair a real Windows host.
2. Open an existing OpenCode session in AgentNexus, continue it, then reopen it
   with `opencode --session <id>` on that machine.
3. Repeat with Codex (`codex resume <id>`) and, if installed, Claude/Gemini.
4. Disconnect the browser, host and hub in turn and confirm the run/session
   recovery behaviour.
5. Run the load/soak and manual accessibility checks before production use.
