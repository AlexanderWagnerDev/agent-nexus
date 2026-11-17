# Verification status

Last updated: 2026-11-16. This file records what was actually executed in the
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
| Command Code 1.89.1 adapter | Detected on Windows 11; 21 real local sessions discovered, a 125-part transcript parsed and search hits returned; the committed JSONL fixture covers user/assistant text, reasoning, tool calls and paired tool results. ACP (`command-code acp`) is wired but not exercised against a real account. |
| Cursor adapter | 67 real local main transcripts discovered on Windows 11; working directories recovered from terminal transcripts, titles and message counts parsed, and transcript/search returned hits. Subagent transcripts are excluded. `cursor-agent` was not installed, so terminal resume and ACP are not real-tested. |
| Devin CLI 3000.13.4 adapter | Official binary inspected in a temporary directory; the SQLite schema (`sessions` + `message_nodes`, `main_chain_id`, `hidden`), the ACP `initialize` handshake and the advertised `session/list` capability were verified. No authenticated Devin session was available, so the transcript reader is fixture-tested and terminal/structured runs are not real-tested. |
| Live Windows host update | The new on-demand `AgentNexus Host Update` task replaced the installed `nexusd.exe` on a real per-user Task Scheduler, restarted it and verified `runtime.json` (0.1.4); the rollback path remains fixture-tested. |
| Audit round 2026-11-13 | New regression tests for MCP bearer clients without the browser marker, search `not_searched` semantics, atomic first-run setup, bounded rate-limit maps, hidden approvals on finished runs, admin queue pause, activity grouping by `updated_at`, FTS pruning of deleted sessions, adapter native-ID validation, Codex JSON-RPC request/response separation, ACP failed-turn state, replay coalescing and updater token scoping pass locally |
| Audit round 2026-11-14 | Hardening round with new regression tests: stale runs and their sessions are closed after a hub restart and re-adopted when a host reports them, finished run buffers are released, host broadcasts respect grants, viewers cannot write through a grant, run-scoped prompt text is not broadcast to other users, `terminal_input`/`approve` are enforced, queue attachments fit the body/WS limits, `X-Forwarded-For` is ignored from public peers, run-ID traversal is rejected before an attachment path is built, structured mode enforces the tested version band, Codex approvals resolve and time out, pty runs end on Unix, a healthy first start after an update is not counted as a failure, interrupted installs do not block a release, and rollbacks block the version. `go test ./...`, `go vet ./...`, `npm test`, the expanded i18n/module-import checks, the Playwright smoke test and the 10k-session virtual test pass locally; CI provides the Linux/macOS/race results |
| Audit round 2026-11-15 | Regression tests for the defects found in this round: a structured run without a session id renders its live stream (the transcript loader no longer returns before the buffer flush), a stop during the startup window is remembered and consumes once, Codex JSON-RPC error responses fail the pending call, ACP attachments pass the shared count/size/MIME validator, attachment directories owned by another local user are refused (Unix), git output is capped while the process runs, a resumed or forked session uses the directory reported by the agent store, an empty `allowed_roots` list fails closed for transcript reads, a second active run for the same session is refused by a single atomic insert, a push endpoint cannot be rebound to another account, and the installers ship `nexusd` **and** `nexus` for development builds. Full `go test ./...` (Linux CI also runs `-race`), `go vet`, `gofmt`, staticcheck, govulncheck, actionlint, `node web/scripts/check-syntax.mjs`, `npm test` (45 tests) and the PowerShell/bash installer syntax checks pass locally. The push to `2bdb01f` passed CI (including the race, browser E2E and new `1.27.x` toolchain jobs), Container, Dev host binaries and CodeQL |
| Windows hidden host task | Reinstalled the per-user `AgentNexus Host` task with the PowerShell launcher; the task action contains `-WindowStyle Hidden`, the daemon's parent is the hidden `powershell.exe`, and `nexusd doctor` reports ok. |
| Release v0.1.11 | Tag `v0.1.11` published on 2026-11-17 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.11`, and its SHA-256 matches `checksums.txt` (`51af0d021c826a1e41e94851c944dced66d1320923f981510938e50affc95ea6`). New and legacy home-only configs resolve `allow_all_roots` to true; custom root lists stay restricted unless the field is set. The session detail reloads on a host session delta, and the conflict dialog offers joining the existing run. Full `go test ./...`, `go vet`, `gofmt`, the JavaScript syntax check and the 58 web tests pass. Docker Hub and GHCR tags `v0.1.11`, `0.1.11`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:bf466d80224a447664a67d9891c383439215d419289a78c1bbd4aa81793f97ae`. |
| Release v0.1.10 | Tag `v0.1.10` published on 2026-11-17 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.10`, and its SHA-256 matches `checksums.txt` (`ce0b58f4a0766a817fb6c786eb03014101b929108edd4bb4c2fe381f95546bd3`). A live OpenCode desktop service at `127.0.0.1:28894` was detected through `service.json`; a shared-run test registers the run, stops it, and leaves the service running. Docker Hub and GHCR tags `v0.1.10`, `0.1.10`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:185cfdc1fc139b5e93b846e7c6f586b5bdab5ec3d474e7c3f4d291fda0bc533b`. |
| Release v0.1.9 | Tag `v0.1.9` published on 2026-11-17 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.9`, and its SHA-256 matches `checksums.txt` (`cac53fc32450f32188c08cb84e6dd37c7162c8973cdd1aa23fdb8348e280627c`). The per-user task still uses `nexusd.exe run --hidden`; `schtasks /End` stops the process and `/Run` restores exactly one instance. With `allowed_roots` covering the test profile and `Q:\GitHub`, a live OpenCode session under `Q:\GitHub` returned its transcript and resolved its run directory; the same session was rejected before the root was added. Docker Hub and GHCR tags `v0.1.9`, `0.1.9`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:7d069601396e486005d0c2c1d108904f82a1b9df83158edf522a5b59d43bb3c5`. |
| Release v0.1.8 | Tag `v0.1.8` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.8`, and its SHA-256 matches `checksums.txt` (`fa7f9c48665fa803e7427e695035297e90714a462c7f898b94ecbbb7293eeba2`). The per-user task now uses `nexusd.exe run --hidden`; the daemon starts hidden, `schtasks /End` stops the process, and `/Run` restores exactly one instance. Docker Hub and GHCR tags `v0.1.8`, `0.1.8`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:b06699e8e37b0b041970c565037ba87a012db6e3a7471ecfb7eb142ef6579d36`. |
| Release v0.1.6 | Tag `v0.1.6` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.6`, and its SHA-256 matches `checksums.txt` (`39908676e1e7bd7582a65407ba747c68fb19282491dd3c1d965e6955136c3019`). Docker Hub and GHCR tags `v0.1.6`, `0.1.6`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:fe421ba56fbeb608791250986896844bdae3cb84a39332497760e1a75702c44e`. |
| Release v0.1.5 | Tag `v0.1.5` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.5`, and its SHA-256 matches `checksums.txt` (`6844f178095d504a013c91a7e423a7da2199aa51ac59ef9eff1045b88b9451ff`). Docker Hub and GHCR tags `v0.1.5`, `0.1.5`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:e6d832545502ba7e4832734fbf18e55e857a9c90480adb19cc3554795e2c946e`. |
| Release v0.1.4 | Tag `v0.1.4` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.4`, and the embedded web assets contain the icon brand mark used on the boot screen, sign-in card and sidebar. Docker Hub and GHCR tags `v0.1.4`, `0.1.4`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:b21cd84613114413ef9b8f9dd3e02f965cc985a56f3c10cd636c946b174877f7`. |
| Release v0.1.3 | Tag `v0.1.3` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.3`. Docker Hub and GHCR tags `v0.1.3`, `0.1.3`, `0.1` and `latest` resolve to the multi-arch digest `sha256:a217df4627b032992bc68e5ea58a49e84149a2d174d6f1bd64949e0ea487c14d`; the moving `alpha` tag pointed there at publication time. |
| Release v0.1.2 | Tag `v0.1.2` published on 2026-11-16 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.2`. Docker Hub and GHCR tags `v0.1.2`, `0.1.2`, `0.1` and `latest` resolve to the multi-arch digest `sha256:7061c549f2402b0d36c14dfcd5c3605c603bf6d30009ca9d5936a53238e385da`; the moving `alpha` tag pointed there at publication time. |
| Release v0.1.1 | Tag `v0.1.1` published on 2026-11-15 by the manual Release workflow; archives, checksums, SBOMs and the cosign bundle are attached to the GitHub release. Docker Hub and GHCR tags `v0.1.1`, `0.1.1`, `0.1`, `alpha` and `latest` resolve to the multi-arch digest `sha256:b8716fdea47d4e0efb346a59fcb2dbb9baf2514eac9499b0c034e43b0d8b734c`. |
| Release v0.1.0 | Tag `v0.1.0` published on 2026-11-15. The manual release workflow produced archives, checksums, SBOMs and a cosign bundle; the draft was reviewed and published. The downloaded Windows archive contains `nexus-hub.exe`, `nexusd.exe` and `nexus.exe`; all three report version `0.1.0`. Docker Hub tags `latest`, `v0.1.0`, `0.1.0`, `0.1`, `alpha` and `sha-77f1c5d` resolve to the same multi-arch digest. |

## Not verified here

- Three real Windows hosts through Portainer, a real browser over the public
  internet, a real host disconnect and a real hub process restart.
- Real Claude Code, Gemini CLI and Cursor Agent sessions. None of those CLIs is
  installed; Cursor desktop transcripts were exercised read-only, and no
  authenticated Devin session was available.
- A real Command Code ACP session. The CLI and 21 local JSONL sessions were
  exercised read-only; starting an ACP turn would use the signed-in account.
- A real OpenCode 1.x CLI. Its store layout is covered by the adapter's tolerant
  reader, but no 1.x binary was available; structured mode is intentionally
  disabled for 1.x.
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
- A live service rollback on a real Windows Task Scheduler, systemd user unit or
  launchd agent. The Windows replacement itself was exercised on a real
  per-user task; rollback remains fixture-tested.
- A real public release feed before v0.1.0 was published. v0.1.0 is the first
  release; channel selection, ETag handling and signature/checksum failure
  paths remain covered by local fake release servers.
- macOS notarization and Windows Authenticode signing: neither exists in
  v0.1.0. The installers rely on SHA-256 and cosign keyless verification.
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
