# Verification status

Last updated: 2026-11-15. This file records what was actually executed in the
development environment. It is an evidence record, not a release gate or a
request for the maintainer to perform tests personally.

## Automated and executed here

| Area | Evidence |
| --- | --- |
| Go unit/integration | `gofmt -l .`, `go vet ./...`, `go test ./...` pass locally (Windows, Go 1.27.4); CI runs Linux, macOS and Windows plus `go test -race` with the minimum version from `go.mod`, and a separate job compiles with the newest supported `1.27.x` toolchain |
| Static analysis | `staticcheck` v0.8.2 passes; `govulncheck` v1.9.0 reports no vulnerabilities; `actionlint` v1.7.12 passes on all workflows |
| Browser unit tests | `node web/scripts/check-syntax.mjs`, `npm test` pass |
| Browser smoke | `E2E_SPAWN_HUB=1 npm run e2e` passes, including bell badge geometry, focus return for palette/notifications and the five-entry mobile bar with a More menu |
| Browser UI regressions | `npm run e2e:demo-ui` (with `E2E_SPAWN_DEMO=1`) covers tab dedupe, cross-host project grouping, the bell badge, host overflow actions, the Git diffstat list and the mobile sticky header |
| Installer syntax | `bash -n scripts/install-host.sh` and the PowerShell parser accept the reworked installers; Docker Compose and both Portainer stacks validate with `docker compose config` when their required variables are set |
| Virtualized 10k sessions | `node e2e/virtual.mjs`: ~20 rows initially, 33 after 1,400 sessions, 222/252 DOM nodes, no duplicates |
| Accessibility | axe-core scan passes desktop (dark and light) and mobile for home, projects, sessions, usage, fan-out, runs, hosts, settings and a structured chat; no critical/serious violations |
| Visual regression | Four stable demo views compare at 0 px; activity views are excluded because elapsed times are live |
| Load/soak harness | `npm run e2e:soak` starts up to 12 synthetic hosts, opens structured runs and samples goroutines, heap and FDs from `/metrics`; a 0.25 min smoke run (2 hosts, 3 runs, 8 samples) showed goroutines 14→16 and no FD growth. |
| Device signing | Ed25519 and P-256/P1363 signature tests, unsigned-write rejection, per-device nonce replay rejection, step-up key registration and the stop/prompt/approval/input paths |
| Multi-host simulation | Three fake hosts, six sessions, three parallel structured runs, host-disconnect isolation and hub-restart metadata persistence |
| Installer/update | Active-run refusal, stale runtime snapshot, checksum and version checks, rollback path, fake-host tests |
| Auto-update | Unit tests for stable/prerelease channel selection, ETag/304 handling, asset naming, interval clamping, exponential backoff, blocked versions, persisted update state, tar extraction of the expected regular file (symlinks ignored), run gating during installation and missing staged binaries. The complete install/restart/rollback path is exercised only against fixtures, not a real service replacement. |
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
| Audit round 2026-11-13 | New regression tests for MCP bearer clients without the browser marker, search `not_searched` semantics, atomic first-run setup, bounded rate-limit maps, hidden approvals on finished runs, admin queue pause, activity grouping by `updated_at`, FTS pruning of deleted sessions, adapter native-ID validation, Codex JSON-RPC request/response separation, ACP failed-turn state, replay coalescing and updater token scoping pass locally |
| Audit round 2026-11-14 | Hardening round with new regression tests: stale runs and their sessions are closed after a hub restart and re-adopted when a host reports them, finished run buffers are released, host broadcasts respect grants, viewers cannot write through a grant, run-scoped prompt text is not broadcast to other users, `terminal_input`/`approve` are enforced, queue attachments fit the body/WS limits, `X-Forwarded-For` is ignored from public peers, run-ID traversal is rejected before an attachment path is built, structured mode enforces the tested version band, Codex approvals resolve and time out, pty runs end on Unix, a healthy first start after an update is not counted as a failure, interrupted installs do not block a release, and rollbacks block the version. `go test ./...`, `go vet ./...`, `npm test`, the expanded i18n/module-import checks, the Playwright smoke test and the 10k-session virtual test pass locally; CI provides the Linux/macOS/race results |
| Audit round 2026-11-15 | Regression tests for the defects found in this round: a structured run without a session id renders its live stream (the transcript loader no longer returns before the buffer flush), a stop during the startup window is remembered and consumes once, Codex JSON-RPC error responses fail the pending call, ACP attachments pass the shared count/size/MIME validator, attachment directories owned by another local user are refused (Unix), git output is capped while the process runs, a resumed or forked session uses the directory reported by the agent store, an empty `allowed_roots` list fails closed for transcript reads, a second active run for the same session is refused by a single atomic insert, a push endpoint cannot be rebound to another account, and the installers ship `nexusd` **and** `nexus` for development builds. Full `go test ./...` (Linux CI also runs `-race`), `go vet`, `gofmt`, staticcheck, govulncheck, actionlint, `node web/scripts/check-syntax.mjs`, `npm test` (45 tests) and the PowerShell/bash installer syntax checks pass locally. The push to `2bdb01f` passed CI (including the race, browser E2E and new `1.27.x` toolchain jobs), Container, Dev host binaries and CodeQL |

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
- PostgreSQL backend (not implemented).
- Container/Portainer runtime in this environment.
- A live service binary replacement and rollback on a real Windows Task
  Scheduler, systemd user unit or launchd agent. The code paths are covered by
  unit/fixture tests, but no real service was replaced in this environment.
- A real public release feed: no release exists during alpha. Channel
  selection, ETag handling and signature/checksum failure paths are covered by
  local fake release servers.
- macOS notarization and Windows Authenticode signing: neither exists in the
  alpha. The installers rely on SHA-256 and cosign keyless verification.
- Browser end-to-end and pixel-diff runs in this round: `web/node_modules` was
  not installed here, so only the dependency-free unit tests and the syntax
  check ran locally. The workflows and the existing Playwright suites are
  unchanged and remain the CI evidence.

## Known limitations reviewed in this round

These are deliberate ceilings, not release gates:

- Tab and session rows still nest a close/tag button inside the link container.
  Clicks behave correctly (the handlers call `preventDefault`), but a stricter
  screen-reader reading of the markup would separate the controls.
- A transcript page is still produced by decoding the whole native transcript
  and then slicing it. The pagination contract is correct; very large JSONL
  sessions cost more memory per request than a streaming reader would.
- The search index treats a per-directory discovery error as an empty
  directory, so an unreadable project folder can prune its documents until the
  30-second full rescan succeeds again. Fixing it needs a partial-failure
  signal from every adapter; search remains a convenience, not the source of
  truth.
- The hub keeps a small per-run state object (sequence cursor) after the output
  buffers are released, so a re-adopted run continues the same cursor. The
  object is bounded and does not hold transcript data.
- Windows Server 2025, three physical hosts and a live service replacement were
  not available in this environment.

## Release policy

This verification record is informational. Publication requires the maintainer's
explicit approval, not a fixed-duration load test, personally executed device
tests, or a formal third-party audit. Existing automated checks remain part of
the ordinary development workflow.
