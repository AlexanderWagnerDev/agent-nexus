# AgentNexus — Agent Integrations

Technical analysis of how AgentNexus discovers existing sessions of each coding agent and continues them **natively**. It defines the adapter contract used by the host daemon ([ARCHITECTURE.md §5](ARCHITECTURE.md#5-agent-adapter-model)) and implements decisions ADR-003, ADR-004 and ADR-005 from [PLAN.md](PLAN.md#7-technical-decisions-adrs).

- **Research date:** 2026-11-10. Agent CLIs change often; every statement below must be re-verified in the Phase 0 spikes ([ROADMAP.md](ROADMAP.md#phase-0--foundations-and-technical-spikes)) and pinned by contract tests (ADR-009).
- **Verification legend:** ✅ confirmed in official vendor documentation · ⚠️ partially confirmed, third-party source, or marked experimental/preview by the vendor · ❓ unverified assumption that must be tested.

## Table of contents

1. [Summary](#1-summary)
2. [Common adapter contract](#2-common-adapter-contract)
3. [Claude Code](#3-claude-code)
4. [Codex CLI](#4-codex-cli)
5. [OpenCode](#5-opencode)
6. [Command Code](#6-command-code)
7. [Gemini CLI](#7-gemini-cli)
8. [Other agents and the generic ACP adapter](#8-other-agents-and-the-generic-acp-adapter)
9. [Support tiers](#9-support-tiers)
10. [Concurrency and "active elsewhere" detection](#10-concurrency-and-active-elsewhere-detection)
11. [Verification backlog](#11-verification-backlog)
12. [Sources](#12-sources)

---

## 1. Summary

| Capability | Claude Code | Codex CLI | OpenCode | Gemini CLI | Generic ACP agent |
| --- | --- | --- | --- | --- | --- |
| Session store | JSONL per session under `~/.claude/projects/<encoded-cwd>/` ✅ | Rollout JSONL files under `$CODEX_HOME` ⚠️ (path not in official docs) | SQLite DB, path via `opencode db path` ✅/⚠️ | Files under `~/.gemini/tmp/<project_hash>/chats/` ✅ | Agent-specific |
| Best discovery interface | Read-only transcript scan + `claude agents --json` ⚠️ | `codex app-server` → `thread/list`, `thread/read` ⚠️ (experimental) | `opencode session list --format json`, `opencode export` ✅ | `gemini --list-sessions` (project-scoped) ✅ | ACP `session/list` (only if advertised) ✅ |
| Native resume (terminal) | `claude --resume <id>` ✅ | `codex resume <id>` ✅ | `opencode --session <id>` ✅ | `gemini --resume <id>` ✅ | Agent-specific |
| Native fork | `--resume <id> --fork-session` ✅ | `codex fork <id>` ✅ | `--session <id> --fork` ✅ | ❓ | ❓ |
| Structured mode | ACP via `claude-agent-acp` ⚠️; fallback `claude -p` stream-json ✅ | `codex app-server` (stdio JSON-RPC) ⚠️ | `opencode serve` HTTP + SSE ✅ (alt. `opencode acp`) | ACP via `--acp` ⚠️ | ACP |
| Remote approvals | ACP `session/request_permission` / `--permission-prompt-tool` ✅ | `item/commandExecution/requestApproval`, `item/fileChange/requestApproval` ⚠️ | `POST /session/:id/permissions/:permissionID` ✅ | ACP ⚠️ | ACP |
| Live state of sessions running outside AgentNexus | `claude agents --json --all` ⚠️ (research preview) + process inspection | Process inspection + file activity (heuristic) ❓ | Process inspection + DB activity (heuristic) ❓ | Process inspection (heuristic) ❓ | ❓ |
| First-class support | Phase 2–4 | Phase 2–4 | Phase 2–4 | Phase 6 | Phase 6 |

**Key insight:** All three first-release agents support resuming by native session ID from the command line. This makes **terminal mode** (ADR-003) a robust, vendor-neutral baseline for native continuation that does not depend on any experimental protocol. Structured mode adds a richer UI but relies on interfaces that are partly experimental — hence it is layered on top, not the foundation.

Cursor and Devin CLI/Desktop are covered in [§8.1](#81-cursor) and [§8.2](#82-devin-cli-and-devin-desktop).

### 1.1 Implementation notes (0.2.0-dev)

Observed against local stores on Windows (2026-11-10), in addition to the
vendor documentation above:

- **Normalization:** Windows working directories are converted to forward
  slashes before reaching the hub, so sessions from different adapter stores
  group under one project. Branch names are filled from a read-only Git lookup
  with a ten-minute per-directory cache, only inside the host policy roots.

- **Codex CLI 0.156.x** writes compressed `.jsonl.zst` rollouts. The adapter
  decompresses them with `klauspost/compress/zstd`; plain `.jsonl` remains
  supported. Titles are taken from the first meaningful user message, skipping
  desktop-injected `<recommended_plugins>` blocks.
- **OpenCode 2.x** stores sessions in a SQLite database at the path reported
  by `opencode debug paths` (`db` line). The adapter opens it read-only and
  reads `session_v2`/`session_message` (falling back to `session`/`message`/
  `part`). `opencode session list` is project-scoped, so it is not used for
  cross-project discovery. OpenCode 2.x has no terminal `--fork` flag;
  AgentNexus uses the native `POST /api/session/{id}/fork` endpoint in
  structured mode instead.
- **Claude Code** is parsed from `projects/*/*.jsonl` with a tolerant parser;
  it was fixture-tested rather than exercised against a local installation.
- **Gemini CLI** discovery is best effort: `~/.gemini/tmp/*/chats/*.json` is
  project-hash scoped, so the adapter warns that the working directory must be
  verified before resuming.
- **OpenCode v2 storage:** `session_v2` and `session_message` are preferred when
  present; the legacy `session`/`message`/`part` tables remain as a fallback.
  A fixture test builds the v2 schema, verifies discovery, transcript, search
  and the `--session` resume command.
- **OpenCode 1.x** is recognized as a supported terminal/discovery/transcript
  band. The structured HTTP driver targets the v2 server API introduced in
  OpenCode 2.0, so structured mode is disabled for 1.x unless the operator
  explicitly allows untested versions. OpenCode 2.0 requires a fork-boundary
  payload; the driver retries with `{"boundary":{"type":"through"}}` when the
  server rejects an empty body.
- **Command Code** sessions are JSONL under
  `~/.commandcode/projects/<project-slug>/`; discovery and transcript read the
  header, message tree and `.meta.json` title. Structured mode uses
  `command-code acp` through the generic ACP driver; terminal mode resumes and
  forks with `--resume <id>` and `--resume <id> --fork-session`.
- **Structured mode:** OpenCode uses `opencode serve`, Codex uses
  `codex app-server --stdio`, and any ACP adapter can be configured with
  `nexusd acp`. Codex `thread/resume` and ACP `session/load` are used for
  existing sessions when the agent advertises `loadSession`.
- **Concurrency:** one shared guard rejects a second run for the same
  `agent:native_id` across terminal, OpenCode, Codex and ACP maps. Per-session
  locks are therefore consistent across modes. `active_elsewhere` is enforced
  by the host on resume; the hub returns a machine-readable 409 and only sends
  `force: true` after a step-up-confirmed dialog.
- **Discovery latency:** the daemon fingerprints the Claude, Codex, OpenCode
  (including `-wal`/`-shm`), Command Code and Gemini store paths every 2 seconds, debounces
  changes for 500 ms and keeps the 15-second full rescan as a fallback. A new
  session normally reaches the hub in 2-3 seconds.
- **Terminal replay:** each terminal run keeps a bounded 1 MiB ring buffer
  (`run_buffer_bytes`, clamped to 64 KiB-16 MiB). Output produced during a hub
  outage is replayed on reconnect and deduplicated by sequence number.
- **Terminal exit:** a run ends when the agent process exits. The daemon drains
  the output already available, then terminates descendants that still hold the
  pty, so a background helper or MCP server cannot leave the run `running`.
- **OpenCode event stream:** when `/api/event` fails five consecutive times the
  run is ended with `run.error`. A shared `opencode serve` owned by a native
  client is only detached from; a private server started for the run is
  stopped. A structured run without an event stream cannot report state.
- **Structured fork:** Codex app-server exposes `thread/fork`; AgentNexus
  uses it in structured mode and links the run to the new native thread. The
  source thread is not modified. OpenCode 2.x is forked through
  `POST /api/session/{id}/fork` and linked the same way; 2.0 needs the
  `through` boundary fallback, while 2.1+ accepts an empty body. This is
  tested against a fake server and the real 2.0/2.1/2.2 OpenAPI contracts, and
  OpenCode rejects a fork of an empty session. ACP fork is
  capability-gated on the unstable `session/fork` advertisement and is
  fake-agent tested only. OpenCode's tested v2 CLI has no documented
  non-interactive terminal fork, so terminal fork is offered for Codex and
  Claude only. Structured fork requests are rejected when the driver or adapter
  cannot perform them.
- **Gemini ACP:** the generic ACP driver works with
  `nexusd acp --agent gemini --command "gemini --acp"` when that flag exists in
  the installed Gemini version. The path is implemented and fake-tested, not
  real-tested; see the status labels in §7.
- **Usage metadata:** Codex reports `thread/tokenUsage/updated` with cumulative
  input, cached input, output and reasoning tokens; OpenCode emits
  `session.usage.recorded` with per-call tokens and USD cost; ACP prompt
  responses carry cumulative session `usage`, and `usage_update` can add a USD
  cost. The hub stores only these numeric fields. No token counts or prices are
  estimated, non-USD ACP costs are not converted, and runs without a report are
  shown as unavailable.
- **Test coverage:** Claude JSONL, Codex rollout, OpenCode v2, Command Code JSONL and policy/concurrency guards have unit tests. Real Codex CLI 0.156.1, OpenCode 2.2.7 and Command Code 1.89.1 stores were exercised on Windows 11; real Claude and Gemini sessions were not available (neither CLI is installed). Linux and macOS are covered by CI
  build/tests only.


## 2. Common adapter contract

### 2.1 Detection

`Detect()` resolves the executable (honoring `PATH`, Windows `PATHEXT` and npm `.cmd` shims, plus the per-user binary directories used by native installers and package managers), runs `<agent> --version` with a 5 s timeout, and resolves configuration/data directories, honoring overrides. On Windows the current user `PATH` is read from the registry as well, because a logon task keeps the environment from its start time. The inventory is re-checked every five minutes, so an agent installed while the daemon runs appears without a restart:

| Agent | Overrides honored |
| --- | --- |
| Claude Code | `CLAUDE_CONFIG_DIR` (moves `projects/`, `jobs/`, daemon state), `CLAUDE_CODE_PROJECT_DIR_NAME` (only together with `CLAUDE_CONFIG_DIR`) ✅ |
| Codex CLI | `CODEX_HOME` ✅ (default `~/.codex` ⚠️ — confirm in SP-2) |
| OpenCode | `OPENCODE_DB` ✅; global paths via `opencode debug paths` ✅ |
| Gemini CLI | ❓ (to be determined in WP-6.1) |

Users can configure additional config directories per agent in the host policy (e.g. several `CLAUDE_CONFIG_DIR` profiles).

### 2.2 Session metadata (normalized)

| Field | Required | Notes |
| --- | --- | --- |
| `native_session_id` | yes | The ID accepted by the agent's resume command |
| `agent`, `agent_version_seen` | yes | Version that last wrote the session, if known |
| `cwd` | yes, if known | Project directory; required to resume in the right place |
| `title` | no | Custom name > generated title > first prompt (truncated) |
| `created_at`, `last_activity_at` | yes | Falls back to file times |
| `git_branch`, `message_count`, `size_bytes` | no | |
| `parent_session_id` | no | For forks/subagents where exposed |
| `source_kind` | no | interactive / non-interactive / SDK / background, if exposed |
| `state`, `active_elsewhere`, `attachable` | yes | See [§9](#9-concurrency-and-active-elsewhere-detection) |
| `parse_warnings` | no | Fail-soft indicator (ADR-005) |

### 2.3 Transcript normalization

Transcripts are mapped to `NormalizedMessage { id, role (user|assistant|system|tool), timestamp, parts[] }`, where parts are `text`, `tool_call {name, input}`, `tool_result {status, output (truncated, paged)}`, `file_change {path, diff}`, `reasoning_summary`, `attachment_ref`. Unknown entry types are kept as `unknown {raw_type}` and hidden by default, so a format change never drops content silently.

### 2.4 Resume semantics (the "native" guarantee)

1. AgentNexus starts the agent's **own** resume command or protocol call with the **native session ID** in the session's **original working directory** (unless it no longer exists — then the user is asked).
2. AgentNexus never writes to the agent's store. After the run, the native CLI shows the continued session with all new turns.
3. Flags that the agent does not persist with the session (e.g. Claude Code's `--mcp-config`, `--add-dir`, `--plugin-dir`, `--settings`) are not reconstructed automatically; per-project launch profiles in the host policy can re-apply them.
4. Permission-bypass flags (`--dangerously-skip-permissions`, `--permission-mode bypassPermissions`, Codex `--dangerously-bypass-approvals-and-sandbox`, OpenCode equivalent settings, …) are **never** added by AgentNexus and are rejected in launch profiles unless the host policy explicitly allows them ([SECURITY.md](SECURITY.md#host-policy)).

## 3. Claude Code

### 3.1 Session storage ✅

- Transcripts: `~/.claude/projects/<project>/<session-id>.jsonl`, where `<project>` is the absolute working directory with every non-alphanumeric character replaced by `-` (e.g. `/Users/me/proj` → `-Users-me-proj`). Names longer than 200 characters are truncated and suffixed with a hash of the full path.
- `CLAUDE_CONFIG_DIR` moves storage; `CLAUDE_CODE_PROJECT_DIR_NAME` (with `CLAUDE_CONFIG_DIR`) pins the project directory name.
- Each line is a JSON object (message, tool use/result, request record, metadata). **The format is internal and changes between versions** — Anthropic explicitly recommends against parsing it directly.
- Default retention: 30 days (`cleanupPeriodDays`), so old sessions disappear from discovery when Claude Code cleans them up. AgentNexus must mark such records as `removed` rather than error.
- Background sessions: supervisor state in `~/.claude/daemon/roster.json` and `~/.claude/jobs/<id>/state.json` — documented as **not a stable interface; do not parse**.

### 3.2 Discovery strategy

| Order | Method | Status | Use |
| --- | --- | --- | --- |
| 1 | **Read-only transcript scan** of `projects/*/*.jsonl` | ✅ location / ⚠️ content | Session list. The session ID is the file name (stable, documented). Extract only: `cwd`, first/last timestamps, git branch, custom/generated title, first user prompt, entry count. Tolerant parser: ignore unknown entry types; read head and tail of large files instead of the full file. |
| 2 | **`claude agents --json --all`** | ⚠️ research preview | Live state: `kind` (interactive/background), `state` (`working`, `blocked`, `done`, `failed`, `stopped`), `status` (`busy`, `waiting`, `idle`), `waitingFor`, `sessionId`, `cwd`, `pid`. Polled every 5 s while the Sessions page is open, otherwise every 60 s. |
| 3 | ACP `session/list` via `claude-agent-acp` | ❓ | Evaluate in SP-1 as an alternative listing source. |
| — | Claude Agent SDK `listSessions()` / `getSessionMessages()` | ✅ | Official, but TypeScript/Python only. Not used in-process (ADR-002). Option: tiny bundled helper if the transcript parser proves too brittle (decision in SP-1). |

**Notes**

- Sessions created with `claude -p` or the Agent SDK are **excluded from the native session picker and from `claude --continue`**, but are resumable by ID. AgentNexus shows them with a `non-interactive` badge.
- Sessions whose first prompt was `/loop` are also hidden from the picker; AgentNexus shows them.
- Duplicate copies of a transcript across projects make Claude's cross-project ID lookup fail ("No conversation found"); the adapter detects duplicates and warns.

### 3.3 Native resume — terminal mode ✅

| Action | Command (cwd = session `cwd`) |
| --- | --- |
| Resume | `claude --resume <session-id>` |
| Resume by transcript path | `claude --resume <absolute-path-to.jsonl>` |
| Fork | `claude --resume <session-id> --fork-session` |
| New | `claude` (optionally `-n <name>`) |
| Attach to running background session | `claude attach <id>` (or `claude --resume <session-id>`, which attaches automatically for background sessions) |

Behavioral details relevant for AgentNexus:

- Resuming restores history, model, agent and — when resumed from a terminal by ID without `-p` — the session's **permission mode**, except that `bypassPermissions` is **not** restored (it starts in the default mode). Plan mode is restored. AgentNexus must not add bypass flags (§2.4).
- `--mcp-config`, `--settings`, `--plugin-dir`, `--fallback-model`, `--add-dir` are not restored; launch profiles can re-apply them.
- Resuming the same session in two terminals without forking **interleaves messages into one transcript** → hard requirement for the concurrency guard ([§9](#9-concurrency-and-active-elsewhere-detection)).
- If the session is a still-running **background session**, `claude --resume` attaches to it instead of loading the transcript; with flags such as `--output-format`, `--permission-mode` or `--model` it refuses with exit code 1. The adapter therefore checks `claude agents --json` first and uses `claude attach <id>` explicitly.
- The "Resume this conversation?" dialog (usage-limit estimate on subscription plans) can appear inside the TUI; in terminal mode the user answers it like locally.

### 3.4 Structured mode

| Option | Status | Pros | Cons |
| --- | --- | --- | --- |
| **A — ACP via `claude-agent-acp`** (preferred, provisional) | ⚠️ in official ACP registry (v0.102.0 at research time), distributed as standalone binaries | One generic ACP client for many agents; permission requests via `session/request_permission`; maintained under the `agentclientprotocol` organization | Extra binary per host; `session/load`/`session/list` support ❓; lags behind Claude Code features |
| **B — Direct `claude -p` stream-json** | ✅ documented | No extra dependency: `claude -p --resume <id> --input-format stream-json --output-format stream-json --verbose --include-partial-messages`; approvals through `--permission-prompt-tool <mcp tool>` served by `nexusd` via `--mcp-config` | Hides sessions it *creates* from the native picker (risk R-07); `-p` runs project hooks/MCP servers without a trust dialog → only allowed in policy-approved roots; must not use `--bare` because bare mode ignores the user's subscription login |

Decision: implement A first, keep B as fallback driver behind the same interface; final decision after SP-1. In both options **new** Claude sessions default to terminal mode so they remain visible in the native picker.

### 3.5 Related vendor feature

Claude Code **Remote Control** connects claude.ai / the Claude app to a locally running session via Anthropic's service. It is single-agent and vendor-hosted; AgentNexus is complementary (self-hosted, multi-agent, multi-host). Both can coexist; AgentNexus does not use Remote Control.

## 4. Codex CLI

### 4.1 Session storage ⚠️

- Official docs: sessions are saved as **rollout files** on disk (avoidable with `--ephemeral`), can be archived or deleted; `CODEX_HOME` holds configuration and authentication.
- Third-party sources (consistent with each other, not official): rollouts at `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-<timestamp>-<id>.jsonl`, plus a SQLite metadata index. ❓ Confirm in SP-2; do not rely on the path when `app-server` is available.

### 4.2 Discovery strategy

| Order | Method | Status | Use |
| --- | --- | --- | --- |
| 1 | **`codex app-server`** over stdio: `initialize` → `initialized` → `thread/list` (cursor pagination; filters `cwd`, `archived`, `sourceKinds`, `modelProviders`, `searchTerm`, sort keys) → `thread/read` (stored metadata/history **without** resuming) | ⚠️ documented in detail, but the CLI reference labels `app-server` **experimental** | Session list and transcripts. One short-lived app-server process per indexing pass; protocol version captured with `codex app-server generate-json-schema` and checked into fixtures. |
| 2 | Read-only scan of rollout files | ⚠️ | Fallback if app-server is unavailable or incompatible. Minimal fields only. |

`thread/list` without a `cwd` filter returns threads from all directories, which matches AgentNexus' cross-project view. Non-interactive (`codex exec`) sessions are distinguishable via `sourceKinds` ⚠️.

### 4.3 Native resume — terminal mode ✅

| Action | Command (cwd = thread `cwd`) |
| --- | --- |
| Resume | `codex resume <SESSION_ID>` (UUID or session name) |
| Include non-interactive sessions | `codex resume <SESSION_ID> --include-non-interactive` (only relevant for picker/`--last`) |
| Fork | `codex fork <SESSION_ID>` |
| New | `codex` |

`codex resume` accepts model and sandbox overrides. Terminal resumes pass none, so the session continues with its own configuration. Structured turns send `model` and `effort` only when the user explicitly changes the live selector; the default is the session's own configuration.

### 4.4 Structured mode — `codex app-server` ⚠️

- Transport: **stdio only** (`--listen stdio://`, default). The WebSocket listener is "experimental and unsupported" and allows unauthenticated non-loopback connections by default → never used.
- Flow: `thread/resume {threadId}` → `turn/start {threadId, input:[{type:"text", text}]}` → notifications `turn/started`, `item/started`, `item/agentMessage/delta`, `item/commandExecution/outputDelta`, `turn/diff/updated`, `turn/plan/updated`, `item/completed`, `turn/completed`, `thread/tokenUsage/updated`.
- Approvals (server → client requests): `item/commandExecution/requestApproval`, `item/fileChange/requestApproval`, `item/permissions/requestApproval`, `mcpServer/elicitation/request`, user-input requests; resolution confirmed by `serverRequest/resolved`.
- Control: `turn/interrupt {threadId, turnId}`, `turn/steer` (add input to an active turn), `thread/fork {threadId, cwd, excludeTurns:true}`.
- Fork: the response's `thread.id` becomes the run's thread; the hub creates
  and adopts a new session row and leaves the source session untouched. This
  was verified against the generated Codex 0.156.1 schema and fake app-server;
  it requires a Codex version whose schema includes `ThreadForkParams`.
- Many fields require `capabilities.experimentalApi = true`; AgentNexus avoids experimental-only methods unless needed and records the dependency per method.
- Authentication: app-server uses the user's existing Codex login on the host. AgentNexus never calls `account/login/*` remotely and never forwards tokens (constraint C2, risk R-08). The vendor documentation contains restrictions on certain authentication setups for commercial or hosted services; AgentNexus' model (user's own machine, user's own login) must be re-checked against current terms before 1.0.
- Multiple subscribers: subscriptions are per connection and a thread unloads after its last subscriber leaves, implying several connections can follow one loaded thread ⚠️ — only relevant within one app-server process; a TUI started separately by the user is a different process and cannot be attached through it ❓.

### 4.5 Related vendor feature

`codex remote-control` exists but is labeled **experimental**; not used by AgentNexus.

## 5. OpenCode

### 5.1 Session storage ✅/⚠️

- Since OpenCode 1.2 (per changelog sources) session data lives in a single SQLite database (`opencode.db` with WAL files; channel-specific names such as `opencode-stable.db` have been reported ⚠️). The authoritative path is printed by **`opencode db path`** ✅ and can be overridden with `OPENCODE_DB` ✅.
- OpenCode 2.x is current (2.2.6 listed in the ACP registry at research time). Whether 2.x changed storage or API shapes ❓ — verify in SP-3; the adapter must support the 1.x and 2.x lines separately if they differ.
- Never open the database for writing. If read-only SQLite access is ever used as a fallback, it must use `mode=ro` and tolerate WAL/locking.

### 5.2 Discovery strategy

| Order | Method | Status | Use |
| --- | --- | --- | --- |
| 1 | **`opencode session list --format json`** (`--max-count/-n`) | ✅ | Session list. Whether it lists **all** projects or only the current directory ❓ — SP-3. |
| 2 | **`opencode export <sessionID>`** (JSON; `--sanitize` redacts sensitive data) | ✅ | Transcript on demand. |
| 3 | **`opencode serve`** on loopback with random port and `OPENCODE_SERVER_PASSWORD`: `GET /session`, `GET /session/:id/message`, `GET /session/status`, `GET /project` | ✅ API documented; cross-project scope ❓ | Alternative when CLI output lacks fields; same server reused for structured mode. OpenAPI document at `/doc` is snapshotted for contract tests. |
| 4 | `opencode db [query] --format json` | ✅ command / ⚠️ schema internal | Last-resort fallback only. |

### 5.3 Native resume — terminal mode ✅

| Action | Command (cwd = session's project directory) |
| --- | --- |
| Resume | `opencode --session <id>` (`-s`) |
| Fork | `opencode --session <id> --fork` |
| Continue last | `opencode --continue` (not used; AgentNexus always passes explicit IDs) |
| Attach TUI to a running server | `opencode attach <url> --session <id>` (with `--password`) |

### 5.4 Structured mode — `opencode serve` ✅

- `nexusd` starts `opencode serve --hostname 127.0.0.1 --port <random>` with a random `OPENCODE_SERVER_PASSWORD` (HTTP basic auth, username `opencode`) per run, and stops it when idle. The password is generated by AgentNexus and passed in the child environment, so a pre-existing variable in the daemon environment cannot suppress OpenCode's startup line or change the credential.
- If an OpenCode desktop or TUI client already runs a service (`<state>/service.json` points at a loopback URL that answers), `nexusd` reuses that service instead of starting a second one. Both clients then share the same event stream, so messages sent through AgentNexus appear in the native chat immediately. A stale or unreachable service file falls back to a private server. The service is never stopped by AgentNexus.
- Verified against OpenCode 2.0.0, 2.1.0, 2.2.7 and 2.4.2: `POST /api/session` creates a session, `POST /api/session/{id}/prompt` sends a prompt, `GET /api/event` streams SSE events, `POST /api/session/{id}/interrupt` aborts, and `POST /api/session/{id}/fork` creates a native fork. 2.0 requires a fork boundary and is handled by the `through` retry. The OpenAPI document at `/doc` and `/openapi.json` is the source for these paths; the contract test starts `serve` against each version and checks the required paths.
- Transcripts: 2.4 moved assistant text, reasoning and tool parts into `session_part` and left the inline `content` of `session_message` empty. The adapter joins both layouts, so history and reasoning still render.
- Streaming events handled by the driver include `session.text.*`, `session.reasoning.*`, `session.tool.*`, `permission.asked/replied`, `session.execution.*`, `session.status` and `session.usage.recorded`. Reasoning deltas are forwarded once; the final ended event is used only when no deltas arrived.
- Permission replies pass `once`, `always` or `reject` through unchanged. Unanswered approvals are denied by the daemon after ten minutes.
- The tested bands are `>= 1.0.0 < 2.0.0` (terminal, discovery and transcript; structured disabled) and `>= 2.0.0 < 3.0.0` (structured driver, including the 2.0 fork-boundary fallback). Untested builds keep terminal mode; structured mode is disabled unless `allow_untested_agent_versions` is set. OpenCode 1.x terminal mode has no fork flag; OpenCode 2.x forks only in structured mode.
- Alternative: `opencode acp` (ACP over stdio) through the generic ACP driver — evaluated in SP-3 against the native API.
- mDNS (`--mdns`) and `--cors` are never enabled by AgentNexus.

## 6. Command Code

- **CLI:** `npm i -g command-code` installs `command-code` (also `cmdc` and
  `commandcode`); on Windows `cmd` is the system shell and is never used. The
  adapter was checked against Command Code 1.89.1.
- **Store:** `~/.commandcode/projects/<project-slug>/<session-id>.jsonl`. The
  first line is `{"type":"session", ...}` with the native id, timestamp and
  cwd; every following line is a tree entry (`type:"message"` with
  `message.role` and `content[]`). `.meta.json` carries the display title and
  parent session. Sidecars (`*.checkpoints.jsonl`, `*.prompts.jsonl`,
  `*.share.json`) are ignored.
- **Discovery, transcript and search** read the JSONL and meta files directly;
  they never start the agent. Normalized parts cover user text, assistant text,
  thinking, tool calls and tool results. Tool results are paired with their
  tool call by `tool_use_id`.
- **Terminal mode:** resume `command-code --resume <id>`; fork
  `command-code --resume <id> --fork-session`; new starts the TUI in the
  session's project directory.
- **Structured mode:** ACP over stdio via `command-code acp`. The daemon
  detects the binary and uses the generic ACP driver. Override it with
  `nexusd acp --agent commandcode --command "command-code acp"`. The ACP driver
  uses `session/load` for resume when advertised; fork is capability-gated on
  `session/fork`.
- **Tested band:** `>= 1.0.0 < 2.0.0`. Outside the band, structured mode is
  disabled with the usual warning unless `allow_untested_agent_versions` is set.
- **Auth:** the ACP and terminal paths reuse the same local login
  (`command-code login`). The headless `COMMAND_CODE_API_KEY` path is not used by
  the structured driver.
- **Status:** discovery, transcript, terminal resume/fork and ACP wiring are
  implemented; the ACP session was not exercised against a real Command Code
  account in the test environment.

## 7. Gemini CLI

- Storage ✅: `~/.gemini/tmp/<project_hash>/chats/`, sessions are **project-specific**; retention via `general.sessionRetention` (default 30 days).
- Discovery: `gemini --list-sessions` lists only the **current project's** sessions ✅. Because `<project_hash>` cannot be mapped back to a path without additional data ❓, discovery runs `--list-sessions` in each project directory known to the host (from policy roots and from other agents' session `cwd`s), or reads a project path from the chat files if present ❓.
- Resume ✅: `gemini --resume <UUID>` (also `--resume <index>`, `--resume` for latest), in the project directory. Delete: `--delete-session`.
- Structured: ACP via `gemini --acp` ⚠️ (flag taken from the official ACP registry entry; older versions used `--experimental-acp`).
- Implementation status: **implemented through the generic ACP driver and fake-tested** with a scripted ACP agent (routing, session creation, prompt, streaming, approvals, interrupt and cleanup). Gemini CLI is not installed in the test environment, so the start flag, version detection and real session continuation are **not real-tested**. Configure it explicitly:
  `nexusd acp --agent gemini --command "gemini --acp"`.
- The version band stays `>= 0.1.0 < 1.0.0`; an outside-band version disables structured mode with the usual warning unless `allow_untested_agent_versions` is set.

## 8. Other agents and the generic ACP adapter

The [Agent Client Protocol](https://agentclientprotocol.com) (ACP) standardizes JSON-RPC communication between clients and coding agents over stdio. The official registry (`https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json`) lists launch commands for many agents, for example (at research time):

| Agent | Registry launch command |
| --- | --- |
| Claude Agent (adapter) | `claude-agent-acp` binary |
| Codex (adapter) | `npx @agentclientprotocol/codex-acp` |
| OpenCode | `opencode acp` |
| Gemini CLI | `gemini --acp` |
| GitHub Copilot CLI | `copilot --acp` |
| goose | `goose acp` |
| Cursor | `cursor-agent acp` |
| Devin CLI | `devin acp` |
| Qwen Code | `qwen --acp …` |
| Kilo, Junie, Mistral Vibe, Kimi, Cline, … | various |

Relevant ACP methods and capability gates ✅:

| Method | Gate | Purpose for AgentNexus |
| --- | --- | --- |
| `initialize` | — | Version and capability negotiation |
| `session/new` | — | New session (`cwd`, MCP servers) |
| `session/load` | `loadSession` | Resume with **full history replay** via `session/update` notifications |
| `session/resume` | `sessionCapabilities.resume` | Resume **without** replay |
| `session/list` | `sessionCapabilities.list` | **Discovery** of existing sessions |
| `session/prompt`, `session/cancel` | — | Turns and interrupt |
| `session/request_permission` (agent → client) | — | Remote approvals |
| `fs/*`, `terminal/*` (agent → client) | client capabilities | AgentNexus advertises **no** fs/terminal client capabilities initially, so agents use their own local tools and permission systems |

Generic adapter behavior:

- Discovery only for agents that advertise `session/list`; otherwise sessions appear only once created through AgentNexus.
- Native resume: `session/load` preferred (gives history), else `session/resume`.
- Terminal mode for such agents requires a user-supplied resume command template in the host policy (e.g. `["myagent","--resume","{id}"]`).
- Agents are launched only from paths configured in the host policy; AgentNexus never downloads and executes registry binaries automatically (supply-chain risk, see [SECURITY.md](SECURITY.md#supply-chain-and-updates)).

**Aider** (no ACP, no session IDs; chat history in `.aider.chat.history.md` per repository, `--restore-chat-history`): terminal mode only, through a custom command template; discovery of history files inside allowed roots is a *Could*.

### 8.1 Cursor

- Storage: `~/.cursor/projects/<encoded-workspace>/agent-transcripts/<session-id>/<session-id>.jsonl`. Each line is a `role` plus `message.content` blocks (`text`, `tool_use`, and occasionally `thinking`). Subagent transcripts live under `<session>/subagents/` and are deliberately not exposed as sessions.
- Working directory: Cursor encodes the workspace path into the project folder name, replacing separators and hyphens with the same character. The adapter therefore reads the real `cwd` from a terminal transcript (`terminals/*.txt`) when one exists and falls back to decoding the folder name with a visible warning. Paths containing hyphens can be decoded incorrectly.
- Resume/new: `cursor-agent --resume <id>` and `cursor-agent`. No documented terminal fork command.
- Structured: `cursor-agent acp` through the generic ACP driver. It is auto-detected when `cursor-agent` or the newer `agent` binary is on `PATH` or in the usual per-user install directories.
- Implementation status: discovery, transcript and search were exercised against a real local Cursor desktop store on Windows 11. The store is discovered even when `cursor-agent` is not on `PATH`, but the agent then stays out of the new-session picker and terminal resume/ACP are unavailable. The `cursor-agent` binary was not installed, so those paths are documented but not real-tested.

### 8.2 Devin CLI and Devin Desktop

- Storage: SQLite `sessions.db`, by default `%APPDATA%\devin\cli\sessions.db` on Windows and `$XDG_DATA_HOME/devin/cli/sessions.db` (or `~/.local/share/devin/cli/sessions.db`) elsewhere. Air-gapped federal builds use `cli-federal`; the adapter checks both channels. Devin Desktop bundles the CLI and shares the same session store.
- Schema: `sessions` holds `working_directory`, `created_at`, `last_activity_at`, `title`, `hidden` and `main_chain_id`; `message_nodes` stores a message forest as `chat_message` JSON. The adapter follows the active chain from `main_chain_id` and falls back to all nodes when the head is missing. Session titles come from `sessions.title`, then `prompt_history`, then the first user message.
- Resume/new: `devin --resume <id>` and `devin`. No documented terminal fork command.
- Structured: `devin acp`. The ACP `initialize` handshake (including `loadSession` and `session/list` capabilities) was verified against Devin CLI 3000.13.4.
- Implementation status: the schema and ACP handshake were verified against the official binary, and the transcript reader is fixture-tested. A Devin Desktop session store is discovered without the CLI, but the agent then stays out of the new-session picker. No authenticated Devin session was available in the test environment, so terminal resume and structured runs are not real-tested.

## 9. Support tiers

| Tier | Agents | Guarantees |
| --- | --- | --- |
| **Tier 1** | Claude Code, Codex CLI, OpenCode | Discovery, transcript, terminal resume/new/fork, structured mode, approvals, live state; contract tests on every PR and nightly; supported version ranges published. |
| **Tier 2** | Gemini CLI, Cursor, Devin CLI/Desktop, selected ACP agents (decided in Q-04) | Discovery where the agent allows it, terminal resume, ACP structured mode; adapter unit/fixture tests, nightly contract tests where available. |
| **Tier 3** | Any ACP agent or custom command | Best effort, capability-driven, no contract tests by the project. |

## 10. Concurrency and "active elsewhere" detection

Writing to a session from two processes at once can interleave or corrupt history (documented for Claude Code). AgentNexus therefore combines:

1. **Own runs:** an exclusive per-session lock inside `nexusd` (authoritative for runs started by AgentNexus).
2. **Foreign processes:** best-effort detection of agent processes started outside AgentNexus:

| Agent | Signals | Confidence |
| --- | --- | --- |
| Claude Code | `claude agents --json --all` → `sessionId`, `pid`, `status`, `state` ⚠️; plus process scan (`claude` processes, their `cwd`, `--resume <id>` in argv) | High for sessions listed by agent view; medium otherwise |
| Codex CLI | Process scan (`codex` processes with `resume <id>` in argv or matching `cwd`) + rollout file modified in the last N seconds ❓ | Medium |
| OpenCode | Process scan (`opencode` processes, `--session <id>` in argv, `cwd`) + session `updated` timestamp ❓ | Medium |
| Others | Process scan + modification time | Low |

Process inspection is intentionally best effort: the daemon reads command lines
with `ps -eo args=` on Linux/macOS and `Get-CimInstance Win32_Process` on
Windows, caches the result for under a minute, and matches the agent name plus
native session id. When it reports a match, the UI labels the session **Open in
native terminal**, explains that the signal is heuristic, and offers read-only,
fork or a forced resume instead of treating the scan as proof.

Decision table when a user requests *resume*:

| Situation | Offered actions |
| --- | --- |
| Not active | Resume (terminal or structured) |
| Active in an AgentNexus run | Join as viewer; request controller lease |
| Active elsewhere and attachable (Claude background session, OpenCode server with known URL) | Attach, view read-only, fork |
| Active elsewhere, not attachable | View transcript read-only, fork, or resume anyway after explicit confirmation (logged) |

## 10.1 Tested version matrix

The single source of truth is `internal/host/versions.go`; a test compares this
block with the code, so update both together.

```text
matrix: claude   >= 2.0.0 < 3.0.0   structured=yes
matrix: codex    >= 0.50.0 < 1.0.0  structured=yes
matrix: opencode >= 1.0.0 < 2.0.0   structured=no
matrix: opencode >= 2.0.0 < 3.0.0   structured=yes
matrix: commandcode >= 1.0.0 < 2.0.0   structured=yes
matrix: gemini   >= 0.1.0 < 1.0.0   structured=yes
```

Outside the band the host removes the `structured` capability and reports a
visible warning; terminal mode keeps working. `allow_untested_agent_versions`
keeps structured mode on and is audited once per daemon start. Unknown versions
are marked with a warning but not disabled, because the range cannot be
compared.

## 11. Verification backlog

Items to resolve in Phase 0 spikes (WP-0.1 … WP-0.3) before the dependent phase starts:

| ID | Item | Spike | Status |
| --- | --- | --- | --- |
| V-01 | Minimal-field extraction from Claude transcripts across the last 3 minor versions on Linux, macOS, Windows | [SP-1](spikes/SP-1-claude.md) | ⚠️ fixture-tested; real versions not installed |
| V-02 | `claude agents --json` availability and field stability; behavior when agent view is disabled | [SP-1](spikes/SP-1-claude.md) | ⚠️ not used or verified |
| V-03 | `claude-agent-acp`: `session/list`, `session/load`, permission flow, distribution and license | [SP-1](spikes/SP-1-claude.md) | ⚠️ fake ACP peer only |
| V-04 | Codex rollout location/format; `thread/list` coverage of interactive + exec sessions; `sourceKinds` values | [SP-2](spikes/SP-2-codex.md) | ⚠️ rollout format yes; `thread/list` not verified |
| V-05 | Codex app-server: approval request shapes, interrupt, behavior while the same thread is open in a TUI | [SP-2](spikes/SP-2-codex.md) | ⚠️ fake app-server only; TUI case open |
| V-06 | OpenCode `session list` scope (all projects?), `export` format, 1.x vs 2.x differences | [SP-3](spikes/SP-3-opencode.md) | ⚠️ v1/v2 reads covered; export unused |
| V-07 | OpenCode server: cross-project session listing, permission events in SSE stream | [SP-3](spikes/SP-3-opencode.md) | ⚠️ fake server only |
| V-08 | Terminal-mode resume for all three agents under Windows ConPTY (rendering, resize, Ctrl-C) | [SP-5](spikes/SP-5-pty.md) | ⚠️ manual Windows run; no visual matrix |
| V-09 | CGO-free process `cwd`/argv inspection on macOS | [SP-1](spikes/SP-1-claude.md), [SP-5](spikes/SP-5-pty.md) | ⚠️ `ps` fallback, not verified on macOS |
| V-10 | Gemini CLI mapping from `<project_hash>` to project path | [SP-3](spikes/SP-3-opencode.md) | ⚠️ best effort; hash mapping not decoded |

## 12. Sources

Official vendor documentation consulted on 2026-11-10:

- Claude Code — Manage sessions: <https://code.claude.com/docs/en/sessions>
- Claude Code — Background sessions and agent view: <https://code.claude.com/docs/en/agent-view>
- Claude Code — Run programmatically (`-p`, stream-json, permission prompts): <https://code.claude.com/docs/en/headless>
- Claude Code — Remote Control: <https://code.claude.com/docs/en/remote-control>
- Claude Agent SDK — Sessions; TypeScript reference: <https://code.claude.com/docs/en/agent-sdk/sessions>, <https://code.claude.com/docs/en/agent-sdk/typescript>
- Codex — App Server: <https://learn.chatgpt.com/docs/app-server> (redirected from developers.openai.com/codex/app-server)
- Codex — CLI reference: <https://learn.chatgpt.com/docs/cli/reference>
- OpenCode — Server: <https://docs.opencode.ai/docs/server>; CLI: <https://docs.opencode.ai/docs/cli>; SDK: <https://opencode.ai/docs/sdk>
- Gemini CLI — Session management: <https://geminicli.com/docs/cli/session-management/>
- Agent Client Protocol — Session setup and schema: <https://agentclientprotocol.com/protocol/session-setup>, <https://agentclientprotocol.com/protocol/schema>; registry: <https://agentclientprotocol.com/registry>
- ACP Go SDK: <https://github.com/coder/acp-go-sdk>

Third-party sources (used only where marked ⚠️): OpenCode 1.2 SQLite migration changelog summaries (learnopencode.com), Codex rollout file layout articles (codex.danielvaughan.com).
