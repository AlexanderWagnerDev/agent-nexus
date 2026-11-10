# AgentNexus — Technical Development Plan

This document is the single source of truth for **what** AgentNexus must do (requirements), **how** it will be built (technical decisions) and **in which steps** (work packages). Component details live in [ARCHITECTURE.md](ARCHITECTURE.md), per-agent details in [AGENT-INTEGRATIONS.md](AGENT-INTEGRATIONS.md), security details in [SECURITY.md](SECURITY.md), and the schedule with acceptance criteria in [ROADMAP.md](ROADMAP.md).

- **Status:** Draft for approval — planning phase, no production code yet
- **Last reviewed:** 2026-11-10

## Table of contents

1. [Scope and vision](#1-scope-and-vision)
2. [Glossary](#2-glossary)
3. [Stakeholders and usage scenarios](#3-stakeholders-and-usage-scenarios)
4. [Functional requirements](#4-functional-requirements)
5. [Non-functional requirements](#5-non-functional-requirements)
6. [Constraints and assumptions](#6-constraints-and-assumptions)
7. [Technical decisions (ADRs)](#7-technical-decisions-adrs)
8. [Technology stack](#8-technology-stack)
9. [Planned repository layout](#9-planned-repository-layout)
10. [Work packages](#10-work-packages)
11. [Quality assurance and testing strategy](#11-quality-assurance-and-testing-strategy)
12. [Build, release and distribution](#12-build-release-and-distribution)
13. [Verification status of external dependencies](#13-verification-status-of-external-dependencies)
14. [Risk register](#14-risk-register)
15. [Open questions](#15-open-questions)

---

## 1. Scope and vision

AgentNexus is a self-hosted system that gives a developer (and optionally a small team) a single web dashboard for all AI coding-agent sessions on all of their machines. It must:

- **discover** sessions that already exist on a host, including sessions created long before AgentNexus was installed and sessions created directly in the native CLI;
- **show** their metadata, live state and transcripts;
- **continue** them using the agent's own native resume mechanism so that the session stays a normal, native session of that agent;
- do all of this **securely** across Windows, Linux and macOS hosts that may be behind NAT.

Guiding principles:

| # | Principle | Consequence |
| --- | --- | --- |
| P1 | **Native first** | AgentNexus never writes to an agent's session store. All mutations go through the agent's CLI or official protocol. |
| P2 | **Content stays on the host** | The hub stores metadata. Transcript content is fetched on demand from the host and not persisted on the hub by default. |
| P3 | **Outbound only** | Hosts connect out to the hub. No inbound ports on hosts. |
| P4 | **Remote control is remote code execution** | Every capability is explicitly granted per host and per user, audited, and secure by default. |
| P5 | **Degrade gracefully** | If a structured integration breaks after an agent update, the terminal mode (PTY) still works. |
| P6 | **Verify, don't assume** | Every external interface is pinned to a tested version range and covered by contract tests. |

## 2. Glossary

| Term | Meaning |
| --- | --- |
| **Coding agent** | A third-party AI coding tool such as Claude Code, Codex CLI or OpenCode. |
| **Hub** | The self-hosted AgentNexus server (`nexus-hub`): web app, API, auth, relay, metadata database. |
| **Host** | A machine (Windows, Linux, macOS, or a WSL distribution) on which coding agents run. |
| **Host daemon** | The AgentNexus process `nexusd` running on a host as the logged-in user. |
| **Agent adapter** | Module inside `nexusd` that implements discovery and control for one coding agent. |
| **Native session** | A session as stored by the coding agent itself, identified by its **native session ID**. |
| **Session record** | The hub's metadata entry for a native session (`host + agent + native session ID`). |
| **Run** | One live process attached to a native session, started or attached by AgentNexus. |
| **Terminal mode** | A run in which the agent's own interactive TUI runs in a pseudo-terminal (PTY) streamed to the browser. |
| **Structured mode** | A run in which the adapter drives the agent through a programmatic protocol (ACP, Codex app-server, OpenCode server) and the web app renders a chat UI. |
| **Controller lease** | The exclusive right of one client to send input to a run; others watch read-only. |
| **Client device** | A browser or native app instance that a user has signed in or paired. |
| **ACP** | [Agent Client Protocol](https://agentclientprotocol.com) — JSON-RPC protocol between clients and coding agents. |

## 3. Stakeholders and usage scenarios

**Primary persona:** a single developer self-hosting AgentNexus for their own 2–10 machines.
**Secondary persona:** a small trusted team (≤ 25 users) sharing a hub, each user with their own hosts and optionally shared hosts.

Key scenarios (each maps to requirements below):

- **S1 — Morning overview:** Open the dashboard and see that Codex on the build server is waiting for approval, Claude Code on the laptop finished overnight, and two OpenCode sessions are idle.
- **S2 — Continue from elsewhere:** Started a Claude Code session on the Windows desktop; later continue exactly that conversation from a MacBook browser; back at the desktop, `claude --resume` still shows it with the new messages.
- **S3 — Approve from the phone:** Get a push notification that an agent wants to run `npm install`; inspect the command and approve or deny it.
- **S4 — Find old work:** Search "rate limiter" across all hosts and agents to find the session in which a rate limiter was designed three weeks ago, and fork it.
- **S5 — Add a machine:** Install `nexusd` on a new Linux box, pair it with a one-time code, and see all its existing sessions appear within a minute.
- **S6 — Lose a device:** A phone is lost; revoke it in the dashboard and all its tokens and pairing keys become invalid immediately.

## 4. Functional requirements

Priorities use MoSCoW: **M**ust, **S**hould, **C**ould, **W**on't (for now). The phase column refers to [ROADMAP.md](ROADMAP.md).

> **Note on requirement provenance:** The repository did not contain an earlier written requirement list. The requirements below consolidate the project description ("self-hosted, multi-host web dashboard to discover, manage, and resume existing AI coding agent sessions across Windows, Linux, and macOS") and the mandatory capabilities named for this planning phase (multi-host support, discovery of existing chats, native resume of existing sessions, native clients for all three operating systems, agent adapters for OpenCode, Claude Code, Codex CLI and further agents, device pairing, encryption, permissions). Any additional mandatory requirement must be added here with an ID before implementation of the affected phase starts.

### 4.1 Hosts and connectivity

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-01 | The system shall support hosts running Windows (10 1809+/11, x86_64 and arm64), Linux (x86_64, arm64; glibc and musl) and macOS (current and previous major version, Apple Silicon and Intel). | M | 1 |
| FR-02 | A host shall be added by pairing: a user creates a one-time pairing code in the web app, and the host daemon redeems it. See [SECURITY.md — Host pairing](SECURITY.md#host-pairing). | M | 1 |
| FR-03 | The hub shall show per host: online/offline state, last seen, OS/architecture, daemon version, detected agents with versions and executable paths. | M | 1 |
| FR-04 | Hosts shall connect outbound only; the hub shall tolerate host reconnects without losing session records. | M | 1 |
| FR-05 | A host shall be renameable, disable-able and revocable from the web app; revocation immediately terminates its connection and runs. | M | 1 |
| FR-06 | WSL distributions shall be supported as separate hosts (separate `nexusd` inside the distribution). | S | 3 |

### 4.2 Agent detection and session discovery

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-10 | The host daemon shall detect installed coding agents (Claude Code, Codex CLI, OpenCode in the first release) including version and executable path, honoring the agents' configuration-directory overrides (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_DB`, …). | M | 1 |
| FR-11 | The host daemon shall discover **existing** native sessions of each supported agent, including sessions created without AgentNexus, and keep the index up to date incrementally (file watching or polling). | M | 2 |
| FR-12 | Each session record shall contain at least: host, agent, native session ID, working directory (project), title/summary, created and last-activity timestamps, git branch (if known), size/message count (if known), and normalized state. | M | 2 |
| FR-13 | The normalized session state shall be one of `idle`, `running`, `waiting_for_input`, `completed`, `failed`, `unknown`, plus a flag `active_elsewhere` when a process outside AgentNexus currently uses the session. | M | 2 |
| FR-14 | The web app shall list, sort and filter sessions by host, agent, project, state and time, across all hosts. | M | 2 |
| FR-15 | Users shall be able to view a read-only transcript of any discovered session (user/assistant messages, tool calls and results, collapsed by default). | M | 2 |
| FR-16 | Full-text search across transcripts shall be available, executed on the hosts (federated) so that content does not have to be stored on the hub. | S | 2 |
| FR-17 | Users shall be able to pin, tag and locally rename session records (stored on the hub only; native titles stay untouched unless the agent offers an official rename interface). | C | 4 |

### 4.3 Native resume and live interaction

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-20 | Users shall be able to **resume an existing session natively**: the host daemon starts the agent's own resume command for that native session ID in the session's working directory. After the run, the session must still be resumable in the native CLI with the new messages present. | M | 3 |
| FR-21 | **Terminal mode:** the resumed agent TUI shall run in a PTY on the host (ConPTY on Windows) and be streamed bidirectionally to an in-browser terminal with resize support. | M | 3 |
| FR-22 | **Structured mode:** for agents with a supported programmatic interface, users shall be able to resume a session in a chat UI with streaming output, tool-call cards, file-diff display, interrupt, and remote tool approvals. | M | 4 |
| FR-23 | Users shall be able to start a new session for any detected agent in a working directory permitted by host policy. | M | 3 |
| FR-24 | Users shall be able to fork an existing session where the agent supports forking natively. | S | 3 |
| FR-25 | Multiple clients shall be able to watch the same run simultaneously; exactly one client holds the controller lease and may send input. Lease transfer shall be explicit. | M | 3 |
| FR-26 | The system shall prevent two concurrent writers on the same native session. If the session is `active_elsewhere`, the user shall be offered: attach (if the agent supports it), read-only view, fork, or cancel. | M | 3 |
| FR-27 | A run shall survive client disconnects: output is buffered on the host and replayed on reconnect (scrollback ring buffer). | M | 3 |
| FR-28 | Users shall be able to stop a run (graceful interrupt first, then termination). | M | 3 |
| FR-29 | Remote approval requests (tool permission prompts) shall show the exact command/file change and offer allow once / deny; "always allow" shall only be offered where host policy permits. | M | 4 |

### 4.4 Notifications

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-30 | The web app shall show real-time state changes (session waiting for input, completed, failed). | M | 2 |
| FR-31 | Users shall be able to receive Web Push notifications for configurable events, without content of the conversation in the push payload by default. | S | 4 |

### 4.5 Users, devices and permissions

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-40 | Users shall authenticate with passkeys (WebAuthn); password + TOTP shall be available as fallback. | M | 1 |
| FR-41 | Every signed-in browser/app shall be registered as a client device that can be listed and revoked. | M | 1 |
| FR-42 | A new client device (e.g. a phone) shall be pairable from an already signed-in device via QR code. | S | 4 |
| FR-43 | Role-based access control: `owner`, `admin`, `member`, `viewer`; per-host permissions (`view`, `read_transcript`, `resume`, `create`, `fork`, `terminal_input`, `approve`, `manage`), defined in [SECURITY.md §9](SECURITY.md#9-authorization-and-permissions). | M (single-user), S (multi-user) | 1 / 5 |
| FR-44 | Host-side policy configured **on the host** (allowed project roots, allowed agents, allowed actions, forbidden permission modes such as permission bypass) that the hub cannot override. | M | 3 |
| FR-45 | An append-only audit log of security-relevant events (logins, pairings, runs, inputs sent, approvals, policy changes). | M | 3 |
| FR-46 | Optional **signed-command mode**: hosts accept control commands only if signed by a paired client device key, so a compromised hub cannot command hosts. | S | 5 |

### 4.6 Operations

| ID | Requirement | Prio | Phase |
| --- | --- | --- | --- |
| FR-50 | The hub shall be deployable as a single binary and as an OCI container image, with SQLite by default and PostgreSQL optional. | M / S | 1 / 5 |
| FR-51 | The host daemon shall be installable as a per-user background service on all three OSes and include `nexusd doctor` diagnostics. | M | 1 |
| FR-52 | Backup/restore of the hub database shall be documented and scriptable. | M | 3 |
| FR-53 | Hub and host daemon shall expose health endpoints, structured logs and optional Prometheus metrics. | S | 3 |
| FR-54 | Signed, verifiable release artifacts; optional self-update of the host daemon with signature verification. | S | 5 |

### 4.7 Explicitly out of scope (Won't, for now)

- Running agents in the cloud or on the hub; managing LLM API keys or subscriptions.
- Converting sessions from one agent format to another.
- Editing or deleting native transcripts (deletion may later be offered only through an agent's official delete command, e.g. `opencode session delete`).
- Real-time collaborative typing by several users in one run (only lease-based control).
- Full IDE features (file editor). A read-only git diff view is a Could for Phase 6.

## 5. Non-functional requirements

| ID | Category | Requirement |
| --- | --- | --- |
| NFR-01 | Security | Secure by default: TLS required for any non-loopback hub listener; no default credentials; all host capabilities off until granted. Details in [SECURITY.md](SECURITY.md). |
| NFR-02 | Privacy | No telemetry. Transcript content is not persisted on the hub unless the user enables caching. Push payloads contain no conversation content by default. |
| NFR-03 | Latency | Terminal keystroke-to-echo round trip ≤ 150 ms p95 when client, hub and host are on the same LAN; ≤ RTT + 50 ms otherwise. |
| NFR-04 | Discovery performance | Initial index of 10 000 sessions per host in ≤ 60 s on a mid-range machine; incremental updates visible in the web app within 5 s of the change on disk. |
| NFR-05 | Scale | One hub instance supports ≥ 50 connected hosts, ≥ 200 concurrent viewer connections and ≥ 50 concurrent runs. |
| NFR-06 | Footprint | Idle host daemon: ≤ 50 MB RSS, ≤ 1 % CPU on average. |
| NFR-07 | Reliability | Automatic reconnect with exponential backoff and jitter; runs continue on the host while the hub is unreachable; no data loss of native sessions under any failure (guaranteed by P1). |
| NFR-08 | Compatibility | Each adapter declares a tested version range of its agent; outside the range it degrades to terminal mode with a visible warning. |
| NFR-09 | Portability | No CGO in the host daemon so that it cross-compiles for all targets from one CI runner. |
| NFR-10 | Accessibility | Web app meets WCAG 2.2 AA for non-terminal views; keyboard navigable; responsive down to 360 px width. |
| NFR-11 | Internationalization | UI in English first; all strings externalized for later translation. |
| NFR-12 | Maintainability | Protocol schemas versioned; backwards compatibility between hub version N and host daemon versions N-1 … N+1 (minor). |
| NFR-13 | Observability | Structured JSON logs with correlation IDs (request, host, run); secrets and conversation content never logged. |
| NFR-14 | Licensing | All dependencies must be compatible with distributing AgentNexus under MIT; licenses checked in CI. |

## 6. Constraints and assumptions

**Constraints**

- C1: The agents are third-party software that changes frequently; several relevant interfaces are explicitly marked experimental or internal by their vendors (see [§13](#13-verification-status-of-external-dependencies)).
- C2: Each host uses the user's own agent installation and the user's own agent login. AgentNexus must never transport, share or store agent credentials (also a terms-of-service concern, see risk R-08).
- C3: The host daemon must run as the user that owns the agent sessions (session stores live in the user's home directory).
- C4: Browsers cannot store secrets as safely as native apps; the web client is the primary client nevertheless.

**Assumptions (to be validated in Phase 0)**

- A1: Every first-release agent offers a documented way to resume a session by ID from the command line. *(Verified from vendor documentation for Claude Code, Codex CLI and OpenCode — see [AGENT-INTEGRATIONS.md](AGENT-INTEGRATIONS.md).)*
- A2: Discovery metadata (ID, cwd, timestamps, title) can be obtained without starting a model turn and without network access. *(Expected; verified per agent in spike SP-1…SP-3.)*
- A3: Running a resumed session in a PTY behaves identically to running it in a local terminal. *(Expected; Windows ConPTY behavior to be verified in SP-5.)*
- A4: The hub runs on infrastructure the user controls (home server, VPS, NAS); reachable by hosts and clients via HTTPS, or within a private network/VPN.

## 7. Technical decisions (ADRs)

Each decision records context, the decision, alternatives and consequences. Decisions marked **(provisional)** must be confirmed by the named Phase 0 spike before the dependent phase starts.

### ADR-001 — Hub + host daemon topology with outbound host connections

- **Context:** Hosts are often behind NAT/firewalls; sessions are local files in user home directories.
- **Decision:** A central hub plus a per-host daemon. The daemon maintains one persistent outbound WebSocket over TLS to the hub; the hub relays between clients and hosts.
- **Alternatives:** (a) Browser connects directly to hosts — requires inbound ports or a VPN on every client; rejected as default but possible via a VPN deployment. (b) Peer-to-peer WebRTC — NAT traversal complexity, TURN still needed; rejected for v1. (c) Agents' own remote features (e.g. Claude Code Remote Control) — vendor-specific, cloud-relayed, single-agent; complementary, not a replacement.
- **Consequences:** Hub is a high-value component (see ADR-010 and [SECURITY.md](SECURITY.md#threat-model)); hosts need no inbound ports.

### ADR-002 — Go for hub and host daemon

- **Context:** The daemon must be a small, robust, cross-compiled service for six OS/arch combinations with PTY support including Windows ConPTY; hub and daemon share protocol code.
- **Decision:** Go (current stable release, pinned with the `toolchain` directive in `go.mod`), CGO disabled.
- **Alternatives:** TypeScript/Node.js — would allow direct use of vendor SDKs (Claude Agent SDK, Codex SDK, OpenCode SDK), but single-binary packaging with native PTY addons (`node-pty`) is fragile and the idle footprint is larger. Rust — excellent fit technically, but slower iteration and fewer ready-made libraries for this use case (ACP has a Rust SDK, but WebAuthn/server ecosystem is smaller).
- **Consequences:** No vendor SDK is used directly in-process; adapters speak documented wire protocols (ACP JSON-RPC, Codex app-server JSON-RPC, OpenCode HTTP/OpenAPI, CLI flags). Contract tests pin these protocols (ADR-009). An optional Node-based helper remains possible for an agent whose only usable interface is a JS/Python SDK.

### ADR-003 — Two run modes: terminal mode (universal) and structured mode (rich)

- **Context:** Native resume must work for every agent, also after agent updates; mobile use needs a chat UI and approvals.
- **Decision:** Terminal mode (agent TUI in a PTY, streamed to xterm.js) is the universal baseline delivered first (Phase 3). Structured mode (Phase 4) is added per agent where a stable programmatic interface exists. Both operate on the same native session ID; a session can switch modes between runs.
- **Consequences:** MVP delivers native resume early and robustly; structured mode can lag behind agent releases without blocking users (principle P5).

### ADR-004 — Agent adapter architecture: discovery and control separated **(provisional, SP-1…SP-4)**

- **Decision:** Each adapter implements two independent interfaces: **Discovery** (list sessions, read transcript, detect live state — must work without starting the agent) and **Control** (start/resume/fork/stop runs in terminal and/or structured mode). Structured control uses, in order of preference:
  1. the agent's **own server protocol** where it offers capabilities beyond ACP — Codex `app-server`, OpenCode `serve`;
  2. **ACP** (via `github.com/coder/acp-go-sdk`) for agents that support it natively or through an official adapter — Claude Code via `claude-agent-acp`, Gemini CLI, Copilot CLI, Goose, …;
  3. none → terminal mode only.
- **Alternatives:** ACP for everything (simpler, one code path) — rejected as the only path because ACP session listing and loading are optional capabilities whose support differs per agent, and Codex/OpenCode native servers expose richer state (thread status, multiple subscribers, permission events).
- **Consequences:** One generic ACP client serves many agents; native drivers exist only for the top agents. Details: [AGENT-INTEGRATIONS.md](AGENT-INTEGRATIONS.md).

### ADR-005 — Prefer official interfaces over parsing internal files

- **Context:** Claude Code documents that its JSONL transcript entry format "is internal … and changes between versions"; Codex and OpenCode similarly treat storage as internal.
- **Decision:** Discovery uses, in order: official listing interfaces (`codex app-server thread/list`, `opencode session list --format json` or the OpenCode server API, `claude agents --json`, ACP `session/list`) → documented storage locations read **read-only** with tolerant parsers that extract only a minimal field set → nothing.
- **Consequences:** Parsers are isolated per agent version, covered by fixture-based tests, and fail soft (record shown with `unknown` fields rather than breaking the index).

### ADR-006 — Transport: WebSocket with Protobuf envelopes for host link; REST + WebSocket for clients

- **Context:** Must traverse common reverse proxies (Caddy, Traefik, nginx), carry binary PTY data and structured events, support flow control.
- **Decision:**
  - Host ↔ hub: one WebSocket (`wss://…/v1/host/connect`) carrying binary frames with Protobuf-encoded envelopes; logical channels multiplexed per run; credit-based flow control.
  - Client ↔ hub: REST/JSON API described in OpenAPI 3.1 (generated TypeScript client) plus one WebSocket per client for events and run streams (JSON control messages, binary frames for terminal data).
- **Alternatives:** gRPC bidi streaming — needs end-to-end HTTP/2 through proxies and grpc-web for browsers; rejected. Server-Sent Events — no client→server stream; rejected for runs.
- **Consequences:** Schemas live in `proto/` (Buf for linting and breaking-change detection) and `api/openapi.yaml`.

### ADR-007 — Application-level host authentication instead of mTLS

- **Decision:** Hosts authenticate with an Ed25519 key pair generated locally at pairing time, using a challenge–response handshake inside the TLS-protected WebSocket. TLS can be terminated at a reverse proxy.
- **Alternatives:** mTLS — breaks when TLS terminates at a proxy and complicates certificate rotation; rejected as default.
- **Consequences:** See [SECURITY.md — Host authentication](SECURITY.md#host-authentication).

### ADR-008 — SQLite by default, PostgreSQL optional

- **Decision:** Hub uses SQLite (WAL mode, pure-Go driver `modernc.org/sqlite`) by default; PostgreSQL support behind the same repository interface in Phase 5. Host daemon uses a local SQLite cache including an FTS5 full-text index for federated search.
- **Consequences:** Zero-dependency single-user deployment; migrations written portable (tool: `pressly/goose` or equivalent, decided in WP-0.4). FTS5 availability in the pure-Go driver is to be confirmed in WP-0.4.

### ADR-009 — Version-pinned adapter contracts

- **Decision:** Each adapter declares `supportedVersions` per agent. CI runs contract tests against the oldest and newest supported agent versions (recorded fixtures plus a nightly job against freshly installed agent releases, without model calls where possible). Generated schemas (`codex app-server generate-json-schema`, OpenCode `/doc` OpenAPI) are checked in and diffed nightly.
- **Consequences:** Breaking agent changes are detected before users report them; unsupported versions fall back to terminal mode.

### ADR-010 — Hub is trusted in v1; signed-command mode as hardening

- **Decision:** In v1 the hub is a trusted component (TLS, strong auth, host-side policies limit blast radius). Phase 5 adds an opt-in signed-command mode in which hosts verify per-device signatures on every control command (FR-46).
- **Rationale:** Full end-to-end encryption with a browser client served by the same hub gives limited protection (a compromised hub can serve malicious JavaScript); native clients benefit fully. See [SECURITY.md — Signed-command mode](SECURITY.md#signed-command-mode-phase-5).

### ADR-011 — Web frontend: React + TypeScript + Vite, embedded into the hub binary

- **Decision:** React with TypeScript, Vite build, TanStack Router/Query, xterm.js (`@xterm/xterm`) for terminals, a virtualized list for large session lists. Static assets embedded into `nexus-hub` via `go:embed`. Installable as a PWA (service worker for shell caching and Web Push).
- **Alternatives:** SvelteKit/SolidJS — viable; React chosen for ecosystem depth (terminal, diff and virtualization components) and contributor familiarity.

### ADR-012 — Host daemon runs as a per-user service

- **Decision:** Linux: `systemd --user` unit (optionally with `loginctl enable-linger`); macOS: LaunchAgent; Windows: per-user logon task (Task Scheduler) with optional tray companion. No root/SYSTEM service.
- **Rationale:** Sessions and agent credentials belong to the user (C3); running privileged would widen the blast radius.

### ADR-013 — Native apps later, as a wrapper of the web app

- **Decision:** The web app (PWA) is the primary client. Native desktop/mobile apps (Phase 6, optional) wrap the same UI (e.g. Tauri 2) and add OS keystore-backed device keys (needed for full signed-command protection) and native notifications.

## 8. Technology stack

Versions are intentionally given as "current stable" here; exact versions are pinned in lockfiles and toolchain files when the code is created (WP-0.4), according to the repository's rule to respect pinned toolchains.

| Layer | Choice | Purpose | Verified? |
| --- | --- | --- | --- |
| Language (backend) | Go, CGO disabled | Hub and host daemon | ✅ |
| HTTP/WebSocket | `net/http`, `github.com/coder/websocket` (or `gorilla/websocket`) | API, WebSocket endpoints | ✅ libraries exist; final pick in WP-0.4 |
| Serialization | Protocol Buffers (`buf` toolchain), JSON for REST | Host link envelopes, API | ✅ |
| PTY | `github.com/creack/pty` (Unix) + ConPTY wrapper for Windows (`github.com/aymanbagabas/go-pty` or `UserExistsError/conpty`) | Terminal mode | ⚠️ Windows library choice and maintenance status to be validated in SP-5 |
| ACP client | `github.com/coder/acp-go-sdk` (Apache-2.0) | Structured mode via ACP | ⚠️ API stability (pre-1.0) — pin version |
| Database | SQLite via `modernc.org/sqlite`; PostgreSQL via `pgx` (Phase 5) | Hub metadata, host cache/FTS | ⚠️ FTS5 support in pure-Go driver to confirm |
| WebAuthn | `github.com/go-webauthn/webauthn` | Passkeys | ✅ widely used |
| Password hashing | Argon2id (`golang.org/x/crypto/argon2`) | Fallback passwords | ✅ |
| Crypto | Go standard library (`crypto/ed25519`, `crypto/ecdh`), `golang.org/x/crypto/chacha20poly1305` | Host keys, secret encryption | ✅ |
| File watching | `github.com/fsnotify/fsnotify` + periodic rescan | Incremental discovery | ✅ (rescan covers platforms where watching is unreliable) |
| Service install | `github.com/kardianos/service` or hand-written unit/plist/task templates | Per-user service | ⚠️ per-user mode on Windows to verify; fallback: own templates |
| Frontend | React, TypeScript, Vite, TanStack Router/Query, xterm.js, Tailwind CSS | Web app | ✅ |
| Push | Web Push (VAPID) via `github.com/SherClockHolmes/webpush-go` | Notifications | ✅ (iOS requires the PWA to be installed to the home screen, iOS 16.4+) |
| Testing | Go `testing` + `testify`, Playwright (pre-installed in CI images), Vitest | All levels | ✅ |
| Release | GoReleaser, cosign/Sigstore signatures, SBOM (Syft) | Distribution | ✅ |

## 9. Planned repository layout

```text
agent-nexus/
├── cmd/
│   ├── nexus-hub/          # hub entrypoint
│   └── nexusd/             # host daemon entrypoint (+ "doctor", "pair", "service" subcommands)
├── internal/
│   ├── hub/                # API, auth, relay, notifications, persistence
│   ├── host/               # daemon core: link, run manager, policy, index
│   ├── adapters/
│   │   ├── claude/         # discovery + control (terminal, ACP)
│   │   ├── codex/          # discovery + control (terminal, app-server)
│   │   ├── opencode/       # discovery + control (terminal, serve)
│   │   └── acp/            # generic ACP adapter (Gemini CLI, Copilot CLI, Goose, …)
│   ├── pty/                # cross-platform PTY abstraction
│   └── protocol/           # generated Protobuf code + helpers
├── proto/                  # .proto sources (Buf module)
├── api/openapi.yaml        # client REST API
├── web/                    # React app
├── deploy/                 # Dockerfile, compose example, systemd/launchd/Task Scheduler templates
├── testdata/fixtures/      # recorded agent stores and protocol transcripts per agent version
└── docs/                   # this documentation
```

## 10. Work packages

Work packages (WP) are the implementation steps. [ROADMAP.md](ROADMAP.md) groups them into phases and defines milestones and acceptance criteria.

### Phase 0 — Foundations and technical spikes

| WP | Description | Output |
| --- | --- | --- |
| WP-0.1 | **SP-1 Claude Code spike:** on all three OSes, list sessions from `~/.claude/projects` (and `CLAUDE_CONFIG_DIR`), extract minimal fields, resume via `claude --resume <id>` in a PTY, read live state via `claude agents --json --all`, test `claude-agent-acp` for `session/list`/`session/load` support. | Spike report + fixtures |
| WP-0.2 | **SP-2 Codex spike:** run `codex app-server` over stdio, call `initialize`, `thread/list`, `thread/read`, `thread/resume`, `turn/start`, handle approval requests; confirm on-disk rollout location as fallback; resume via `codex resume <id>` in PTY. | Spike report + generated JSON schema |
| WP-0.3 | **SP-3 OpenCode spike:** `opencode session list --format json` across projects, `opencode export`, `opencode serve` on loopback with password, `GET /session`, SSE `/event`, permission responses; confirm whether one server instance lists sessions of all projects; resume via `opencode --session <id>`. | Spike report + OpenAPI snapshot |
| WP-0.4 | Repository scaffolding: Go module, toolchain pinning, Buf, OpenAPI skeleton, web app skeleton, lint configs (golangci-lint, ESLint, Prettier), license check, CI pipeline (build matrix for 6 targets), migration tool decision, confirm FTS5 in SQLite driver. | Green CI on empty skeleton |
| WP-0.5 | **SP-4 Protocol spike:** host link prototype (WebSocket + Protobuf + multiplexing + flow control) through Caddy, Traefik and nginx with TLS termination. | Latency/throughput numbers |
| WP-0.6 | **SP-5 PTY spike:** PTY abstraction with resize, signals and process-tree termination on Linux, macOS and Windows (ConPTY), running each agent's TUI; evaluate Windows libraries. | Library decision |
| WP-0.7 | Threat model review workshop based on [SECURITY.md](SECURITY.md); finalize ADRs; update docs with spike results. | Accepted ADRs |

### Phase 1 — Hub, host daemon and pairing

| WP | Description |
| --- | --- |
| WP-1.1 | Hub skeleton: config (file + env), HTTP server, health endpoint, structured logging, SQLite + migrations, embedded web assets. |
| WP-1.2 | First-run setup (create owner account), passkey registration/login, password + TOTP fallback, session cookies, CSRF protection, rate limiting. |
| WP-1.3 | Client device registry and revocation. |
| WP-1.4 | Host pairing flow (code creation in UI, `nexusd pair <hub-url>`), host key storage (OS keychain where available, file with 0600 permissions otherwise). |
| WP-1.5 | Host link: authenticated WebSocket, heartbeat, reconnect with backoff, protocol version negotiation. |
| WP-1.6 | Agent detection on host (PATH resolution incl. Windows `PATHEXT` and npm `.cmd` shims, version probing with timeouts, config-dir overrides). |
| WP-1.7 | Per-user service installation for Linux, macOS, Windows; `nexusd doctor`. |
| WP-1.8 | Web app: login, hosts page (status, agents), pairing wizard, devices page. |

### Phase 2 — Session discovery and transcript viewer

| WP | Description |
| --- | --- |
| WP-2.1 | Discovery framework in daemon: adapter registry, incremental indexer (watch + periodic rescan), local SQLite cache, change feed to hub. |
| WP-2.2 | Claude Code discovery adapter (see [AGENT-INTEGRATIONS.md §3](AGENT-INTEGRATIONS.md#3-claude-code)). |
| WP-2.3 | Codex CLI discovery adapter ([§4](AGENT-INTEGRATIONS.md#4-codex-cli)). |
| WP-2.4 | OpenCode discovery adapter ([§5](AGENT-INTEGRATIONS.md#5-opencode)). |
| WP-2.5 | Live-state detection (process inspection + agent-specific status interfaces) → normalized states. |
| WP-2.6 | Normalized transcript model and on-demand transcript fetch (paged) through hub. |
| WP-2.7 | Federated full-text search (host-side FTS5, hub fan-out with timeouts, merged ranking). |
| WP-2.8 | Web app: session list (virtualized, filters, live updates), session detail with transcript viewer, search UI. |

### Phase 3 — Native resume in terminal mode (MVP)

| WP | Description |
| --- | --- |
| WP-3.1 | Run manager in daemon: spawn in PTY with correct cwd and environment, lifecycle, scrollback ring buffer, graceful stop. |
| WP-3.2 | Per-agent resume/new/fork command builders (terminal mode). |
| WP-3.3 | Concurrency guard: session locks, `active_elsewhere` detection, attach-vs-fork decision flow. |
| WP-3.4 | Stream relay through hub with flow control, multiple viewers, controller lease. |
| WP-3.5 | Host policy file (`nexusd` config): allowed roots, agents, actions, forbidden flags; enforcement and UI display. |
| WP-3.6 | Audit log (hub) with hash chaining; host-side local audit file. |
| WP-3.7 | Web app: terminal view (xterm.js, resize, copy/paste, mobile key bar), new-session dialog (host, agent, directory picker limited to allowed roots), run list. |
| WP-3.8 | Backup/restore documentation and commands; Docker image; compose example with Caddy. |
| WP-3.9 | WSL support documentation and testing. |

### Phase 4 — Structured mode, approvals and notifications

| WP | Description |
| --- | --- |
| WP-4.1 | Normalized event model (message delta, tool call, tool result, file change, approval request, plan, usage, state). |
| WP-4.2 | Codex structured driver over `codex app-server` (stdio). |
| WP-4.3 | OpenCode structured driver over `opencode serve` (loopback, random port, random password). |
| WP-4.4 | Generic ACP driver; Claude Code via `claude-agent-acp`. |
| WP-4.5 | Approval broker: pending approvals, timeouts (default deny), policy constraints, audit. |
| WP-4.6 | Web Push (VAPID), notification preferences, content-free payloads. |
| WP-4.7 | Web app: chat view, tool cards, diff viewer, approval dialogs, mobile layout, QR device pairing (FR-42). |

### Phase 5 — Hardening, multi-user and 1.0

| WP | Description |
| --- | --- |
| WP-5.1 | Multi-user RBAC and per-host grants; invitations. |
| WP-5.2 | Signed-command mode (device keys, host-side verification, key rotation). |
| WP-5.3 | PostgreSQL support. |
| WP-5.4 | Release signing, SBOM, reproducible builds, signed daemon self-update. |
| WP-5.5 | Security review / external penetration test; fix findings. |
| WP-5.6 | Performance and soak tests against NFR targets; documentation completion. |

### Phase 6 — Additional agents and native apps

| WP | Description |
| --- | --- |
| WP-6.1 | Gemini CLI adapter (discovery + terminal + ACP). |
| WP-6.2 | Generic ACP agents from the ACP registry (capability-driven). |
| WP-6.3 | Optional native apps (Tauri 2 desktop and mobile) with OS-keystore device keys. |
| WP-6.4 | Optional read-only git status/diff per project. |

## 11. Quality assurance and testing strategy

| Level | Scope | Tooling |
| --- | --- | --- |
| Unit | Parsers, state machines, policy evaluation, crypto helpers | Go `testing`, table-driven; Vitest for web |
| Fixture/contract | Each adapter against recorded session stores and protocol transcripts for every supported agent version | `testdata/fixtures/<agent>/<version>/` |
| Live contract (nightly) | Install latest agent releases in CI containers/VMs, run discovery and protocol handshakes that need no model call; diff generated schemas | GitHub Actions scheduled workflow |
| Integration | Hub + daemon + fake agent (scripted TUI and scripted ACP/app-server peers) | Go integration tests |
| End-to-end | Browser flows (login, pair, discover, resume, approve) | Playwright |
| Cross-platform | Daemon test suite on Linux, macOS and Windows runners | CI matrix |
| Security | SAST (`gosec`, CodeQL), dependency scanning (`govulncheck`, npm audit), fuzzing of parsers and protocol decoders (`go test -fuzz`), secret scanning | CI |
| Performance | Discovery of synthetic 10k-session stores, relay throughput/latency | Benchmarks + soak tests |

**Definition of Done** for every work package: code reviewed; tests for new behavior; docs updated (this folder); no new lint or security findings; cross-platform CI green; ADRs updated if a decision changed.

## 12. Build, release and distribution

- **Artifacts:** `nexus-hub` (linux/amd64, linux/arm64; darwin and windows for development), `nexusd` (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64), OCI image for the hub, web app embedded.
- **Packaging:** archives + checksums; Homebrew tap and winget/Scoop manifests (Phase 5); Debian/RPM packages optional.
- **Signing:** cosign signatures and SBOMs for all artifacts; macOS notarization and Windows Authenticode signing for `nexusd` (required to avoid Gatekeeper/SmartScreen friction; certificate costs are an open question).
- **Versioning:** SemVer; protocol version negotiated independently of product version.
- **Release channels:** `stable` and `next`.

## 13. Verification status of external dependencies

Legend: ✅ verified in official vendor documentation (2026-11-10) · ⚠️ partially verified / third-party sources / marked experimental by vendor · ❓ unverified assumption.

| Interface | Status | Note |
| --- | --- | --- |
| Claude Code `--resume <id>`, `--continue`, `--fork-session`, transcript location `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, `CLAUDE_CONFIG_DIR` | ✅ | Transcript **entry format is internal** and may change on any release |
| Claude Code `claude agents --json [--all]` | ⚠️ | Documented, but agent view is a **research preview** |
| Claude Code `-p --output-format stream-json --input-format stream-json`, `--permission-prompt-tool` | ✅ | Sessions created with `-p`/SDK are hidden from the native picker and `--continue` |
| Claude Agent SDK `listSessions()`, `getSessionMessages()` | ✅ | TypeScript/Python only (not used in-process, see ADR-002) |
| `claude-agent-acp` (ACP adapter) | ⚠️ | Listed in the official ACP registry; per-method support (`session/list`, `session/load`) ❓ to verify in SP-1 |
| Codex `codex resume [SESSION_ID]`, `codex fork`, `codex exec resume` | ✅ | |
| Codex `codex app-server` (`thread/list`, `thread/read`, `thread/resume`, `turn/start`, approvals) | ⚠️ | Documented in detail, but the CLI reference labels `app-server` **experimental**; WebSocket transport "experimental and unsupported" → use stdio only |
| Codex on-disk rollouts `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` | ⚠️ | Official docs say "rollout files" without a path; path from third-party sources |
| OpenCode `--session/-s`, `--continue`, `--fork`, `session list --format json`, `export`, `serve` HTTP API, `acp`, `db path` | ✅ | |
| OpenCode storage `~/.local/share/opencode/opencode.db` (SQLite, since 1.2) | ⚠️ | Third-party sources; use `opencode db path` instead of hard-coding. OpenCode 2.x changes to storage ❓ |
| OpenCode server: one instance lists sessions of all projects | ❓ | Verify in SP-3 |
| Gemini CLI `--resume`, `--list-sessions`, storage `~/.gemini/tmp/<project_hash>/chats/`, ACP via `--acp` | ✅ / ⚠️ | ACP flag taken from the ACP registry entry |
| ACP `session/load`, `session/list`, `session/resume` capabilities | ✅ | Capability-gated; support varies per agent |
| `coder/acp-go-sdk` | ✅ exists, Apache-2.0 | Pre-1.0, frequent releases → pin |

## 14. Risk register

Probability (P) and impact (I): H/M/L.

| ID | Risk | P | I | Mitigation |
| --- | --- | --- | --- | --- |
| R-01 | Agent updates change internal storage formats and break discovery. | H | M | ADR-005 (official interfaces first), tolerant parsers, fixtures per version, nightly live contract tests, fail-soft indexing. |
| R-02 | Experimental agent protocols (Codex app-server, ACP adapters, Claude agent view) change or disappear. | M | M | Terminal mode always available (P5); version ranges (ADR-009); schemas diffed nightly. |
| R-03 | Two writers on the same session corrupt or interleave history (Claude documents interleaving when resumed in two terminals). | M | H | Session locks, `active_elsewhere` detection, attach/fork choice (FR-26). |
| R-04 | Compromised hub → remote code execution on all hosts. | L | H | Host-side policies, no permission-bypass modes remotely by default, audit, signed-command mode (ADR-010), hardened deployment guide. See [SECURITY.md](SECURITY.md). |
| R-05 | Windows ConPTY quirks (rendering, resize, process-tree kill, `.cmd` shims) degrade terminal mode. | M | M | Spike SP-5 on real Windows 10/11 machines; Windows CI runner; job objects for process-tree termination. |
| R-06 | macOS privacy protection (TCC) blocks access to projects in `~/Documents`, `~/Desktop`, iCloud Drive. | M | M | `nexusd doctor` detects it; documentation for granting access; signed/notarized binary for stable TCC identity. |
| R-07 | Sessions created by AgentNexus in Claude structured mode (`-p`/SDK-based) do not appear in the native picker. | H | L | Start new Claude sessions in terminal mode by default; show native session ID/name; document `claude --resume <id>`. |
| R-08 | Terms-of-service constraints on automating agents that use subscription logins (vendor documentation, e.g. for the Codex app-server, restricts certain authentication setups for commercial or hosted services). | M | H | AgentNexus never handles agent credentials; each user runs agents on their own hosts with their own login; document clearly; legal review before any hosted offering (not planned). |
| R-09 | Prompt injection in repository content leads the agent to request dangerous actions that a user approves on a small phone screen. | M | H | Approval UI shows full command, cwd and risk hints; dangerous-command heuristics; "always allow" restricted by policy. |
| R-10 | Browser key storage weaker than native → signed-command mode has limited value on web. | H | M | Documented limitation; native apps (ADR-013) for high-assurance users. |
| R-11 | Large transcripts (tens of MB) make viewing/searching slow. | M | L | Paged transcript API, lazy tool-output loading, host-side indexing. |
| R-12 | Scope creep from supporting many agents. | H | M | Tiered support model in [AGENT-INTEGRATIONS.md](AGENT-INTEGRATIONS.md#8-support-tiers); generic ACP adapter instead of bespoke integrations. |
| R-13 | Code-signing certificates (Apple, Windows) cost money and add release friction. | M | L | Open question Q-03; unsigned builds possible for early phases with documented verification via cosign. |

## 15. Open questions

| ID | Question | Needed by |
| --- | --- | --- |
| Q-01 | Is multi-user support required for 1.0 or is single-user enough (moving WP-5.1 later)? | Phase 5 start |
| Q-02 | Should transcript caching on the hub be offered at all (privacy vs. offline viewing of offline hosts)? | Phase 2 start |
| Q-03 | Budget for Apple Developer ID and Windows code-signing certificates? | Phase 5 |
| Q-04 | Which additional agents beyond the first three are mandatory (Gemini CLI, Copilot CLI, Cursor CLI, Goose, Aider)? | Phase 6 start |
| Q-05 | Is an official AgentNexus CLI (`nexusctl`) desired for scripting? | Phase 5 |
