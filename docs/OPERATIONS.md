# AgentNexus — Operations

Practical runbook for the 0.1.x implementation. It intentionally describes the
current build, not the 1.0 target.

## Data locations

| Component | Default location |
| --- | --- |
| Hub database | `<config-dir>/AgentNexus/hub.db` (override with `--data-dir` / `NEXUS_DATA_DIR`) |
| Hub web assets | embedded in `nexus-hub` |
| Host config and key | `<config-dir>/AgentNexus/nexusd.json` (mode `0600` where the OS supports it) |
| Host session cache | in memory in 0.1.x; rebuilt from native stores on restart |
| Hub audit log | `audit` table in `hub.db` (hash-chained) |

`<config-dir>` is `%APPDATA%` on Windows, `~/.config` on Linux, and
`~/Library/Application Support` on macOS.

## Backup and restore

The hub stores metadata only, but it also stores users, hosts, device sessions,
pairing history and the audit log — back it up.

With the hub stopped (safest):

```bash
cp -a "$NEXUS_DATA_DIR/hub.db" backup/hub-$(date +%F).db
```

With the hub running, use SQLite's online backup:

```bash
sqlite3 "$NEXUS_DATA_DIR/hub.db" ".backup 'backup/hub-$(date +%F).db'"
```

Restore:

```bash
systemctl --user stop agentnexus   # or stop the container / scheduled task
cp backup/hub-YYYY-MM-DD.db "$NEXUS_DATA_DIR/hub.db"
chmod 600 "$NEXUS_DATA_DIR/hub.db"
systemctl --user start agentnexus
```

The host daemon needs no backup: its config file is the only persistent state,
and native sessions remain owned by the agents.

## Upgrade

1. Back up `hub.db`.
2. Replace `nexus-hub` and restart it. Migrations run automatically on start.
3. Replace `nexusd` on each host and restart the per-user service:
   `nexusd service install` or restart the existing unit/task.
4. Verify with `nexusd doctor` and `GET /healthz`, `GET /readyz`.

If a release changes the host-link protocol major version, update hosts before
or together with the hub; the hub refuses mismatched majors with an upgrade
hint.

## Reverse proxy and TLS

Use Caddy, Traefik or nginx with WebSocket support. The bundled
[deploy/Caddyfile](../deploy/Caddyfile) is a minimal example:

```bash
cp .env.example .env
docker compose up -d --build
```

Do not expose the hub directly on a public interface without TLS. The hub marks
cookies `Secure` when `NEXUS_PUBLIC_URL` starts with `https://` or when
`--tls-cert` is used.

## First-run and recovery

- The first browser that completes `/api/v1/setup` becomes the `owner`.
- If the owner password is lost and TOTP is enabled, there is currently **no
  self-service recovery**. Stop the hub, back up `hub.db`, and use a SQLite
  client to remove the user/session rows, then complete setup again. This is an
  administrative operation with shell access; it is not exposed over the API.
- Revoking a host closes its link and prevents it from reconnecting. The host
  must be paired again with a new code.
- Revoking a device deletes its sessions immediately and closes its open
  browser WebSocket.

## Passkeys

Passkeys require a **hostname** and a secure browser context. `localhost` works
for development; an IP address such as `127.0.0.1` does not satisfy WebAuthn's
relying-party rules. For production set:

```bash
nexus-hub --public-url https://nexus.example.com ...
```

Then register a passkey under **Devices → Passkeys**. Password + optional TOTP
remains available as fallback. The relying-party ID is the public URL hostname;
changing the hostname invalidates existing passkey registrations.

## Device pairing

A signed-in device can create a QR/link pairing token under **Devices → Pair a
device**. Tokens are single-use, expire after two minutes and create a separate
revocable device session. Anyone who obtains the link during its lifetime can
pair, so treat it like a temporary password.

## Structured mode

OpenCode structured runs start a loopback-only `opencode serve` process per
run with a random port and server password. The password is only held in
memory for that run; the server is killed when the run stops. If a run remains
after a daemon crash, inspect and terminate the orphaned `opencode serve`
process before resuming that session.

Claude Code and Codex CLI currently use terminal mode only.

## Web Push

Enable notifications per device under **Devices**. Requirements:

- HTTPS (localhost is treated as a secure context by browsers) and a browser
  with Push support; iOS requires the PWA to be installed on the home screen.
- Browser notification permission; if it was previously denied, reset the site
  permission in the browser first.
- The hub generates a VAPID key pair on first start and stores it in
  `hub.db`. Back it up with the database; replacing it invalidates existing
  subscriptions.

The payload contains only a generic title, a generic body and the run URL. It
never contains transcript text, commands, paths or approval details.

## Resource notes

- `nexusd` runs as the logged-in user and must not run as root/SYSTEM.
- On Windows, `nexusd service install` writes a per-user Startup launcher
  (`%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\AgentNexus.vbs`)
  that starts `nexusd run` hidden at logon. It needs no administrator rights.
  It is not a Windows service and does not restart a crashed process; use Task
  Scheduler/NSSM if you need that.
- On macOS, the launch agent may need Full Disk Access / Files and Folders
  permission to read project directories under `~/Documents` or `~/Desktop`.
  `nexusd doctor` reports missing or unreadable agent stores.
- Native process detection is heuristic and can miss sessions started through
  a wrapper that does not keep the agent name and session ID in its command
  line.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Host stays offline | `nexusd status`, hub logs, outbound TLS/WebSocket path through the proxy |
| Pairing fails | code expired after 10 minutes, already used, or host clock skew; create a new code |
| No sessions discovered | `nexusd discover --json`; verify agent config-dir overrides and `nexusd doctor` |
| Resume rejected | cwd outside `allowed_roots`, agent not allowed, or forbidden permission-bypass flag |
| Transcript empty | host offline, unsupported agent version, or parser warning; check host logs |
| Terminal blank | run may still be starting; check run state, host logs, TTY size |
| Audit chain warning | restore from backup and investigate; the `audit` table was modified |
