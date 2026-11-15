# Changelog

All notable changes to AgentNexus are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

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

[0.1.1]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.1
[0.1.0]: https://github.com/AlexanderWagnerDev/agent-nexus/releases/tag/v0.1.0
