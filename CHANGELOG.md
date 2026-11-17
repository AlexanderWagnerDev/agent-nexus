# Changelog

All notable changes to AgentNexus are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

## [0.1.14] - 2026-11-18

### Fixed

- Windows: the host task detaches from its console instead of only hiding the
  window. The Task Scheduler logon trigger could create the console before the
  desktop was ready, leaving a `nexusd` window visible at logon.

## [0.1.13] - 2026-11-17

### Added

- OpenCode API contract test and `Agent contracts` workflow: starts a real
  `opencode serve` for 2.0.0, 2.1.0, 2.2.0 and 2.3.0, checks the session,
  prompt, event, interrupt, permission and fork endpoints, and exercises the
  OpenCode 2.0 fork-boundary fallback.
- CI runs the Go test suite on Linux, Windows and macOS, adds a fuzz smoke job
  for every fuzz target, and `scripts/test-all.ps1` runs the same quality gates
  locally (core, `-Full` with race and fuzzing, `-E2E` with Playwright).

### Changed

- OpenCode structured mode now starts with the v2 API in 2.0.0 instead of
  2.2.0; OpenCode 2.0 requires a `through` fork boundary, which the driver sends
  as a fallback when the server rejects an empty body. OpenCode 1.x remains
  terminal/discovery/transcript-only.

### Fixed

- Windows host updates no longer fail when the scheduled task has already
  stopped the daemon before `taskkill` runs; a missing process is ignored during
  replacement and rollback.

## [0.1.12] - 2026-11-17

### Added

- Cursor adapter: discovers agent transcripts under
  `~/.cursor/projects/*/agent-transcripts`, renders transcripts and search hits,
  resumes with `cursor-agent --resume <id>` and uses ACP through
  `cursor-agent acp` when the binary is on `PATH`. Cursor does not record a
  reliable workspace path in every transcript, so the adapter recovers it from
  terminal metadata and marks folder-name fallbacks. Cursor desktop stores are
  discovered read-only even when the CLI is not on `PATH`.
- Devin CLI and Devin Desktop adapter: reads the shared SQLite session store
  (`%APPDATA%\devin\cli\sessions.db` on Windows,
  `$XDG_DATA_HOME/devin/cli/sessions.db` elsewhere), follows the active
  `message_nodes` chain, renders reasoning/tool calls/results, resumes with
  `devin --resume <id>` and uses ACP through `devin acp`. Devin Desktop shares
  the CLI session store and is discovered without the CLI as well.
- Web: Cursor and Devin brand marks, labels and glyph colours in the agent
  lists, session/usage filters and fan-out targets.
- Web: a transcript detail setting (chat, normal, full); full mode shows
  reasoning blocks for OpenCode legacy, Claude and Codex.
- Web: session transcripts refresh from the host without a manual page reload.

### Changed

- Session detail joins an existing writable chat instead of offering a
  read-only transcript, and reclaims the write lease before each prompt.

## [0.1.11] - 2026-11-17

### Changed

- Host policy: `allow_all_roots` now defaults to true for new installations and
  for existing configurations that still use the old home-directory-only
  default. `allowed_roots` remains available as an opt-in exposure filter by
  setting `allow_all_roots` to false. The host still enforces the flag locally;
  it is not a sandbox and does not limit what a running agent may read.

### Fixed

- Web: the read-only session transcript reloads when the host reports a
  session change, such as a message written in the native OpenCode client,
  instead of waiting for a manual refresh.

## [0.1.10] - 2026-11-17

### Fixed

- OpenCode chats now reuse the HTTP service an already running OpenCode
  desktop or TUI client uses. The host previously started a private server per
  run; it wrote the same SQLite store, but the native client never received
  those events, so messages sent through AgentNexus only appeared after the
  native chat was reopened. If no native service answers, the host still starts
  a private server as before.

## [0.1.9] - 2026-11-17

### Fixed

- Host: sessions whose working directory is outside `allowed_roots` are no
  longer advertised to the hub. The web UI used to list them and then failed
  every transcript read, run start and git view with "session is outside the
  allowed roots". Codex sessions under the home directory worked while
  OpenCode sessions in project roots did not, which made the failure look
  agent-specific. The policy itself is unchanged; the snapshot now matches it.

## [0.1.8] - 2026-11-16

### Fixed

- Windows: `GetConsoleWindow` is loaded from `kernel32.dll` instead of
  `user32.dll`. The 0.1.7 daemon panicked when the scheduled task started it
  with `run --hidden`, leaving the host offline. The hidden-start path now
  works and the task action remains `nexusd.exe run --hidden`.

## [0.1.7] - 2026-11-16

> **Withdrawn:** `GetConsoleWindow` was loaded from `user32.dll`, so the
> Windows hidden-start task panicked. Use `v0.1.8`.

### Fixed

- Windows: the host task now runs `nexusd.exe run --hidden` directly instead of
  through a PowerShell wrapper. The wrapper could leave the daemon running when
  Task Scheduler stopped the task, which kept the binary locked during updates.
  The daemon hides its own console window, and the update/uninstall helpers
  stop a stray daemon before replacing files.

## [0.1.6] - 2026-11-16

### Fixed

- Windows: the per-user `AgentNexus Host` task now starts `nexusd` through a
  hidden PowerShell launcher. No console window opens, so closing a terminal
  window can no longer disconnect the host. Existing installations can switch
  the task once with `nexusd service install`.

## [0.1.5] - 2026-11-16

### Added

- Hub/Web: hosts can be checked and updated from the Hosts page. A per-host
  action installs an available `nexusd` update, and "Update all" checks every
  online host and offers to update them together.
- Hub/Host: an admin with host manage permission can enable or disable
  `updates.allow_remote_install` from the host page (step-up confirmation).
  The host still verifies checksums and optional signatures, waits for idle
  runs and keeps the channel, signature policy and release source local.
- Host: Command Code is detected as `commandcode` when `command-code`, `cmdc`
  or `commandcode` is installed. Discovery and transcript read
  `~/.commandcode/projects`, terminal mode resumes/fork with
  `command-code --resume`, and structured mode uses `command-code acp` through
  the generic ACP driver.
- Host: OpenCode 1.x is now a recognized terminal/discovery/transcript band.
  Structured mode remains limited to OpenCode 2.2+ because the HTTP server API
  differs; `allow_untested_agent_versions` can override this explicitly.

### Changed

- Host updates no longer require the external `cosign` binary. SHA-256 is
  always verified; when `cosign` is installed the signed bundle is verified
  too, and `updates.require_signature=true` makes that mandatory. This lowers
  the default trust mode for hosts without cosign from signed to checksum-only.

### Fixed

- Windows: the installer now adds `%LOCALAPPDATA%\AgentNexus` to the user PATH,
  so `nexusd` and `nexus` work in a new terminal without the full path.
- Windows: host updates now run the binary replacement in a separate on-demand
  task (`AgentNexus Host Update`). This avoids the Task Scheduler job object
  blocking the helper with `Access is denied` when it tries to break away.
- Web: a host whose update failed because `cosign` is missing now shows the
  host-local remediation command instead of only the raw error.

## [0.1.4] - 2026-11-16

### Changed

- Web: the boot screen, the sign-in card and the sidebar show the app icon
  instead of a plain letter tile, so the brand matches the favicon and the
  installed PWA icon.

### Fixed

- Hub: a host that missed a read deadline was reported as a bare `502` whose
  body was the raw Go error, with no log line, so a busy host looked like a
  broken hub. Host round-trips now answer `504` with `reason=host_timeout` when
  the deadline trips and `503` with `reason=host_offline`/`host_busy` when the
  link cannot take the request; every failure is logged with host, method and
  timeout, and the message is readable in the browser.
- Web: the session view awaited the transcript round-trip without a catch, so
  one timed-out request replaced the whole page with an error. The metadata,
  actions and tabs stay usable and only the transcript box shows the failure.
- Host: the incremental search index re-read the transcript of a session that a
  live turn was still writing on every refresh, competing with the transcript
  read the hub serves. Sessions changed within the last 30 seconds are skipped
  and indexed once the turn goes quiet.
- Host: an OS hostname change now updates the name the host reports, and the
  hub follows it unless an admin renamed the host. The reported name is stored
  separately, so the next reconnect no longer overwrites a rename.
- Windows: the host installer no longer aborts on PowerShell 5.1 when the
  status probe writes to stderr before the pairing check.
- Web: the install one-liner generated by the Pair-a-host dialog pins the hub's
  release tag (`main` for development builds) instead of resolving the newest
  stable release through the GitHub API, which failed on rate limits and
  blocked networks.

## [0.1.3] - 2026-11-16

### Fixed

- Host: a transcript that filled the search-index body limit could panic
  `transcriptBody` with a negative slice bound, killing `nexusd` while the
  background index was building and leaving the host offline. The body is now
  clamped to the limit, and index-refresh panics are recovered so a malformed
  transcript cannot take the host down.
- Web: the last host row's action menu was clipped by the list container,
  hiding rename and revoke. The host list no longer clips its overflow.

## [0.1.2] - 2026-11-16

### Added

- The hub's Pair-a-host dialog now shows install-and-pair one-liners for
  Linux, macOS and Windows. Each command downloads the official installer,
  pairs the host with this hub and installs the per-user service. The
  installer is pinned to the hub's release tag (`main` for development
  builds); the manual `nexusd pair` command remains available for existing
  installations.

### Changed

- Release automation: the Release workflow creates and pushes the annotated
  tag itself and publishes the GitHub release directly, without a draft stage
  or confirmation input.
- Container builds once and pushes the same multi-arch manifest to GHCR and
  Docker Hub. Release dispatches check out the tagged commit even after
  `main` moved and no longer attach a misleading `sha-` tag.

## [0.1.1] - 2026-11-15

### Fixed

- Home, Hosts and the new-session picker no longer crash on a fresh hub. The
  hosts API returns an empty array instead of `null`, which previously failed
  on `hosts.sort(...)` in the browser.
- Host: an explicitly empty `allowed_roots` list fails closed for transcript
  reads; structured runs bind to the native session reported by the driver;
  adapter search results respect the 1..100 bound; a hub signing key that is
  not 32 bytes is rejected before pinning; the process list is lowercased once
  per snapshot instead of once per session/process pair.
- Hub: task status changes are guarded by the delegated run id, so a late
  status report from another run cannot overwrite a finished task.
- Web: diff hunks keep patch-like content such as `-- old` followed by
  `++ new`; large terminal pastes are chunked instead of dropped; the
  live-event cursor resets after a hub restart; modals close on navigation;
  the highlighter keeps its capture groups; the clipboard falls back to
  `execCommand`.
- Web: the command palette reopens after navigation, and buffered events from
  a previous hub epoch are dropped.

### Changed

- Web: the TOTP secret popup after first-run setup was removed. The secret it
  displayed could not be confirmed and was replaced on enrollment. TOTP is
  enrolled under Settings -> Devices, which issues and verifies a fresh secret.

## [0.1.0] - 2026-11-15

First official release: hub, host daemon, SAS host pairing, session discovery
and native resume, structured OpenCode/Codex/ACP chat, projects, pinned
sessions, tasks, usage, fan-out, multi-user grants, passkeys, Web Push, signed
host auto-updates with rollback, the read-only MCP endpoint and the `nexus`
terminal client.

[0.1.14]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.14
[0.1.13]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.13
[0.1.12]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.12
[0.1.11]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.11
[0.1.10]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.10
[0.1.9]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.9
[0.1.8]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.8
[0.1.7]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.7
[0.1.6]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.6
[0.1.5]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.5
[0.1.4]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.4
[0.1.3]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.3
[0.1.2]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.2
[0.1.1]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.1
[0.1.0]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.0
