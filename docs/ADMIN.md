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
implicitly has all of them. The host policy is enforced independently on the
machine and cannot be widened from the hub.

Sensitive actions require a fresh step-up confirmation valid for five minutes:

- creating host pairing codes and revoking hosts;
- revoking devices and creating device pairing links;
- changing users/roles and host grants;
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

`allowed_agents` applies to new and resumed sessions in terminal, OpenCode,
Codex and ACP modes. Paths are canonicalized (symlinks resolved) before the
root check. `process_grace_seconds` (default 5, maximum 60) is how long a
stopped run may take to exit before its whole process tree is killed.
`nexusd doctor` prints the current policy summary.

## Agent support

| Agent | Discovery | Native terminal resume | Structured mode |
| --- | --- | --- | --- |
| OpenCode | SQLite v1/v2 | `opencode --session <id>` | `opencode serve` |
| Codex CLI | rollout JSONL/JSONL.ZST | `codex resume <id>` | `codex app-server` |
| Claude Code | JSONL projects | `claude --resume <id>` | ACP adapter (optional) |
| Gemini CLI | best effort | `gemini --resume <id>` | not implemented |
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

## Search and export

Search queries online hosts in parallel; a host without `read_transcript` is
not queried. Results can be filtered by agent and host. Offline hosts are shown
as "not searched", never silently ignored.

Transcript export is available as Markdown or JSON from a session. It is read
from the host on demand, requires `read_transcript`, and is bounded at 10,000
messages per export. The hub never persists transcript content.

## Audit

The hub audit is append-only and hash-chained. **Audit** verifies the chain for
the displayed range and shows logins, step-ups, host/device changes, grants,
runs and approvals. Hosts also write a local JSONL audit under their config
directory. Ship both to your central log system if required.

Back up `hub.db` regularly; a restore rewinds users, hosts, sessions metadata,
audit and settings together. See [OPERATIONS.md](OPERATIONS.md) for SQLite and
Docker volume commands.

## Updates and rollback

1. Back up the database.
2. Pull the new `NEXUS_TAG` and restart the hub.
3. Replace `nexusd` on hosts and rerun `nexusd service install`.
4. Verify `/healthz`, `/readyz`, `nexusd doctor`, a terminal run and a structured
   run.

Database migrations run automatically. There is no automatic downgrade; keep
the previous container tag and database backup for rollback.

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
- PostgreSQL, native mobile/desktop apps and manual WCAG 2.2 AA certification are open (the automated scan is clean).
- Claude ACP was not exercised against a real Claude installation in this
  release; terminal mode is verified.
- Linux/macOS real-agent sessions were not available for testing; CI covers
  builds and unit tests.
- Terminal output produced while the hub is offline is not replayed; the native
  session and process survive.
- Process-tree termination covers Unix process groups and Windows Job Objects;
  the Windows job assignment has a small post-start race (see [SECURITY.md](SECURITY.md)).
- Windows Server 2025 uses the same Task Scheduler schema but was not available
  for testing.
- The release is a pre-release while the Claude and cross-OS real-agent gaps
  remain.

## Troubleshooting

See [OPERATIONS.md](OPERATIONS.md#troubleshooting) and
[INSTALL.md](INSTALL.md#troubleshooting).
