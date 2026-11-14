# Administrator guide

This is the day-2 guide for an AgentNexus hub. Installation is covered in
[INSTALL.md](INSTALL.md); platform details, backups and troubleshooting in
[OPERATIONS.md](OPERATIONS.md).

## Users, roles and step-up

| Role | Capabilities |
| --- | --- |
| owner | All hub administration, creates users, manages roles; only one owner is created at setup |
| admin | Sees all hosts and sessions, can manage users' grants and revoke devices/hosts |
| member | Sees hosts explicitly shared with them; can create hosts and sessions subject to grants |
| viewer | Read-only access to shared hosts and sessions; cannot add hosts or start runs |

Per-host grants are the unit of least privilege: `view`, `read_transcript`,
`resume`, `create`, `fork`, `terminal_input`, `approve`, `manage`. A host owner
implicitly has all of them. A grant never raises a viewer's role: write
capabilities in a viewer grant are dropped on save, and any grant implies
`view` so the host stays visible in the lists. `terminal_input` is required to
hold the controller lease and send prompts/keystrokes; `approve` is required to
answer approval requests. The host policy is enforced independently on the
machine and cannot be widened from the hub.

Sensitive actions require a fresh step-up confirmation valid for five minutes:

- creating host pairing codes and revoking hosts;
- renaming/disabling a host and requesting a host update installation;
- revoking devices, registering device keys and creating device pairing links;
- changing users/roles and host grants;
- creating or revoking API tokens;
- registering passkeys or enabling TOTP.

A registered passkey is preferred; password + TOTP is the fallback. Step-up is
bound to the current browser session, so another browser must confirm again.

## Host policy

Edit `<config-dir>/AgentNexus/nexusd.json` on the host and restart `nexusd`.
The effective default is the current user's home directory, all detected agents,
new sessions and fork enabled, with permission-bypass flags rejected.

```json
{
  "policy": {
    "allowed_roots": ["/home/me/code", "/home/me/work"],
    "allowed_agents": ["claude", "codex", "opencode"],
    "allow_new_sessions": true,
    "allow_fork": true,
    "allow_force_resume": true,
    "process_grace_seconds": 5,
    "forbidden_args": [
      "--dangerously-skip-permissions",
      "--dangerously-bypass-approvals-and-sandbox",
      "--yolo",
      "--auto-approve"
    ]
  }
}
```

`allow_force_resume` (default `true`) controls whether the hub may resume a
session that the host's best-effort process scan reports as open in another
terminal. Setting it to `false` rejects every `force` run, even from a signed
hub command and even after a browser confirmation; rejected attempts are
audited on the host.

`allowed_agents` applies to new and resumed sessions in terminal, OpenCode,
Codex and ACP modes. Paths are canonicalized (symlinks resolved) before the
root check. `process_grace_seconds` (default 5, maximum 60) is how long a
stopped run may take to exit before its whole process tree is killed.
`nexusd doctor` prints the current policy summary.

### Agent versions

`nexusd doctor` prints the tested version matrix. A detected version outside
the band removes the `structured` capability and shows a warning in the hub;
terminal mode is unaffected. Set `"allow_untested_agent_versions": true` to
keep structured mode on anyway; each use is written to the host audit log.
Unknown or unparseable versions are warned about but not disabled.

### Search index

The daemon keeps a local SQLite FTS5 cache in
`<config-dir>/AgentNexus/search-index.db` so repeated federated searches do
not re-read every agent store. Only sessions inside `allowed_roots` for an
allowed agent are indexed. The file contains transcript text, is created with
`0600` permissions and is never sent to the hub. Disable it with
`"search_index": false` in `nexusd.json`; delete or rebuild it with
`nexusd index rebuild`, and inspect it with `nexusd index status`.

## Agent support

| Agent | Discovery | Native terminal resume | Structured mode |
| --- | --- | --- | --- |
| OpenCode | SQLite v1/v2 | `opencode --session <id>` | `opencode serve` |
| Codex CLI | rollout JSONL/JSONL.ZST | `codex resume <id>` | `codex app-server` |
| Claude Code | JSONL projects | `claude --resume <id>` | ACP adapter (optional) |
| Gemini CLI | best effort | `gemini --resume <id>` | ACP via `gemini --acp` (fake-tested, not real-tested) |
| Other ACP agents | adapter-dependent | configured command | `nexusd acp` |

Native resume uses the original session ID and the agent's own store/context.
AgentNexus does not copy chats into a new session.

## Active runs and approvals


- **Runs** shows all live terminal/structured runs the user can see.
- One native session can have only one AgentNexus run on a host, regardless of
  mode. A second attempt is rejected before a process starts.
- The first browser to open a run acquires the controller lease. Another
  device can take over; the previous device becomes read-only.
- Approval requests come from the agent itself. Allow-once/deny is forwarded to
  the agent; unanswered requests default to deny after ten minutes.
- Closing the browser does not stop the agent; it reconnects to the same run.

## Notifications

Enable Web Push per device under **Settings** or **Devices**. Notifications are
content-free and are sent for approvals, completed runs, failures and host
disconnects. Requirements: HTTPS (localhost is allowed by browsers), notification
permission and, on iOS, an installed PWA. The VAPID key lives in `hub.db`; keep
it in backups or existing subscriptions become invalid.

Under **Settings → Notifications**, global category switches apply to every
host. A per-host override replaces the global value for that host only; the
override is stored per user and host, and **Use global defaults** removes it.
In-app notifications are always recorded regardless of push settings.

## Search and export

Search queries online hosts in parallel; a host without `read_transcript` is
not queried. Results can be filtered by agent and host. Offline hosts are shown
as "not searched", never silently ignored.

Transcript export is available as Markdown or JSON from a session. It is read
from the host on demand, requires `read_transcript`, and is bounded at 10,000
messages per export. The hub never persists transcript content.

## Tasks and delegation

Tasks are visible only to their owner (admins may see all). Creating a task
does not start anything. Delegation requires the task's project, host and agent
for a new session, or an existing session for resume/fork, and re-checks the
user's host grant. Unfinished dependencies block delegation; dependency cycles
are rejected. Delegated prompts use the same durable prompt queue as interactive
chats, so a host disconnect does not lose the prompt. A task whose run vanished
across a hub restart is marked failed; a task with a live run remains running.

## Host health and diagnostics

The host detail page derives `healthy`, `degraded`, `offline`, `pending` or
`disabled` from reported inventory, index, update and policy state. Reasons are
machine-readable codes such as `agent_untested_version`, `index_error`,
`policy_no_roots` or `host_offline`; the hub never probes a host for the page.
Administrators can open **Settings -> Diagnostics** for hub uptime, database
ping, counts, host health, updates and recent failures. The redacted JSON export
requires step-up and omits host identities, session ids, actors and audit
details. The host detail page can also run the host-side doctor on demand; that
response is sanitized (no paths, ids or keys).

## Audit

The hub audit is append-only and hash-chained. **Audit** verifies the chain for
the displayed range and shows logins, step-ups, host/device changes, grants,
runs and approvals. Hosts also write a local JSONL audit under their config
directory. Ship both to your central log system if required.

Back up `hub.db` regularly; a restore rewinds users, hosts, sessions metadata,
audit and settings together. See [OPERATIONS.md](OPERATIONS.md) for SQLite and
Docker volume commands.

## Updates and rollback

1. Back up `hub.db`, `secret.key` and `hub-signing.key`.
2. Pull the new `NEXUS_TAG` and restart the hub.
3. Host daemons update themselves on their local channel when idle. Check
   `nexusd update --status`; a manual `nexusd update --install` is optional.
4. Verify `/healthz`, `/readyz`, `nexusd doctor`, a terminal run and a structured
   run.

The host page shows the installed and available versions, channel, automatic
update and signature mode, current phase and last error. A blocked host version
can be retried only on that host (`nexusd update --configure --unblock <version>`).
The hub may request installation only when the host has
`updates.allow_remote_install=true`; it cannot alter the release source or
signature policy.

Database migrations run automatically. There is no automatic downgrade; keep
the previous container tag, host `nexusd.previous` binary and database backup
for rollback. Do not update a hub container across a schema change without a
backup.

## Security checklist

- HTTPS reverse proxy with WebSockets; no direct public hub port.
- Register a passkey, keep password + TOTP as fallback.
- Restrict `allowed_roots` and `allowed_agents` per host.
- Do not expose `nexusd` or agent stores; hosts connect outbound.
- Revoke lost devices and hosts promptly.
- Keep the host key file (`nexusd.json`) `0600`; it is not yet stored in the OS
  keystore.
- Run the hub as the nonroot container user; mount `/data` persistently.
- Monitor the audit page and host logs.

## Known limitations

- Signed-command mode is not implemented; the hub remains trusted for run
  commands.
- PostgreSQL and native mobile/desktop apps are not implemented (the automated accessibility scan is clean).
- Claude ACP was not exercised against a real Claude installation in this
  release; terminal mode is verified.
- Linux/macOS real-agent sessions were not available for testing; CI covers
  builds and unit tests.
- Terminal output produced while the hub is offline is not replayed; the native
  session and process survive.
- Process-tree termination covers Unix process groups and Windows Job Objects.
  The Windows child is created suspended and resumed after job assignment, so
  grandchildren cannot escape that window. Residual risks are an undocumented
  resume API and a failed job assignment; see [SECURITY.md](SECURITY.md).
- Windows Server 2025 uses the same Task Scheduler schema but was not available
  for testing.
- Official release publication depends on maintainer approval, not personally
  performed cross-OS acceptance or prescribed extended tests.

## Troubleshooting

See [OPERATIONS.md](OPERATIONS.md#troubleshooting) and
[INSTALL.md](INSTALL.md#troubleshooting).
