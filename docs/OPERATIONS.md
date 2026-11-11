# AgentNexus — Operations

Practical runbook for the current alpha build. It describes what the code does
and what has actually been tested, not the 1.0 target.

## Data locations

| Component | Default location |
| --- | --- |
| Hub database | `<config-dir>/AgentNexus/hub.db` (override with `--data-dir` / `NEXUS_DATA_DIR`) |
| Hub secret key | `<data-dir>/secret.key` (AES-256-GCM key for TOTP and VAPID secrets, mode `0600`) |
| Hub signing key | `<data-dir>/hub-signing.key` (Ed25519 identity for signed host commands, mode `0600`) |
| Hub web assets | embedded in `nexus-hub` |
| Host config and key | `<config-dir>/AgentNexus/nexusd.json` (mode `0600` where the OS supports it) |
| Host audit log | `<config-dir>/AgentNexus/audit.log` (JSONL) |
| Host session cache | in memory; rebuilt from the native agent stores on restart |
| Hub audit log | `audit` table in `hub.db` (hash-chained) |

`<config-dir>` is `%APPDATA%` on Windows, `~/.config` on Linux and
`~/Library/Application Support` on macOS. Native agent sessions stay in the
agents' own stores; the hub never rewrites them.

## Docker image and GHCR

Development images are published to GitHub Container Registry. There is no
official version yet: `alpha` tracks `main`, and `sha-<short>` pins a commit.

```bash
docker pull ghcr.io/alexanderwagnerdev/agent-nexus:alpha
docker pull ghcr.io/alexanderwagnerdev/agent-nexus:sha-167a829   # pinned build
```

The image is multi-arch (`linux/amd64`, `linux/arm64`), runs as the distroless
nonroot user (UID 65532), writes only to `/data`, and exposes a
`/nexus-hub --healthcheck` probe.

Minimal compose deployment with Caddy:

```bash
cp .env.example .env
# set HUB_DOMAIN and HUB_NAME in .env
docker compose up -d
docker compose ps
```

The bundled compose publishes only Caddy on ports 80/443 and keeps the hub on
an internal Docker network.

### Portainer behind Nginx Proxy Manager or another proxy

Use [deploy/portainer-stack.yml](../deploy/portainer-stack.yml). Create a
Portainer stack, paste the file, and set:

- `HUB_DOMAIN` — the public HTTPS hostname. WebAuthn passkeys require a
  hostname, not an IP address.
- `HUB_PORT` — the host port the proxy points at (default `8787`).
- `NEXUS_TAG` — `alpha` for the development channel or `sha-<short>` to pin a commit. There is no official release yet.

Nginx Proxy Manager: forward the hostname to `<docker-host>:8787`, enable
WebSockets, and enable TLS. Caddy: use the provided
[deploy/Caddyfile](../deploy/Caddyfile) or an equivalent reverse-proxy block.
Do not expose the hub directly on a public interface without TLS.

### Volume ownership

Docker initializes a new named volume with the ownership of `/data` in the
image (UID 65532). If you bind-mount a host directory instead, make it writable
by UID 65532:

```bash
sudo chown -R 65532:65532 /srv/agentnexus
```

## Backup and restore

The hub stores users, hosts, device sessions, pairing history, settings and the
audit log. Native session transcripts remain on the hosts.

The database, `secret.key` and `hub-signing.key` form one backup set. TOTP secrets and the VAPID
private key cannot be read without the key file.

With the hub running, use the built-in online snapshot (available in the hub
binary, including the container image):

```bash
nexus-hub --data-dir "$NEXUS_DATA_DIR" --backup "backup/hub-$(date +%F).db"
cp -a "$NEXUS_DATA_DIR/secret.key" "backup/secret-$(date +%F).key"
cp -a "$NEXUS_DATA_DIR/hub-signing.key" "backup/hub-signing-$(date +%F).key"
```

The command refuses to overwrite an existing file. With the hub stopped, a
plain copy of `hub.db` and `secret.key` is also consistent. For Docker, execute
the snapshot inside the container so `/data` is the mounted volume:

```bash
docker compose exec nexus-hub /nexus-hub --data-dir /data --backup /data/backup-$(date +%F).db
docker compose cp nexus-hub:/data/secret.key backup/secret.key
```

Restore (stop the hub first and restore both files):

```bash
systemctl --user stop agentnexus          # or stop the container/task
cp backup/hub-YYYY-MM-DD.db "$NEXUS_DATA_DIR/hub.db"
cp backup/secret-YYYY-MM-DD.key "$NEXUS_DATA_DIR/secret.key"
cp backup/hub-signing-YYYY-MM-DD.key "$NEXUS_DATA_DIR/hub-signing.key"
chmod 600 "$NEXUS_DATA_DIR/hub.db" "$NEXUS_DATA_DIR/secret.key" "$NEXUS_DATA_DIR/hub-signing.key"
systemctl --user start agentnexus
```

Rollback: keep the previous hub binary and the matching backup set. Stop the
hub, restore the database and key, start the previous binary against the same
data directory. Do not mix an old database with a new secret or signing key. If the signing key
is intentionally rotated, remove `hub_public_key` from each host config (or
re-pair) so the new key can be pinned. A host daemon
needs no backup beyond `nexusd.json`; native sessions are owned by the agents.

## Host updates

The host can self-update without a hub-managed binary feed:

```bash
nexusd update --check --channel prerelease
nexusd update --install
nexusd update --rollback
```

Downloads come from GitHub releases, not the hub. The archive SHA-256 is
verified against `checksums.txt`; the cosign bundle is verified when `cosign`
is installed. Hosts without cosign must explicitly pass
`--allow-checksum-only`. The private release repository needs a read-only
`NEXUS_GITHUB_TOKEN` with Contents access.

`--install` is refused while `runtime.json` reports active runs. It requires an
installed service, keeps `nexusd.previous`, and restarts the service. Windows
logs to `%APPDATA%\AgentNexus\update.log`. A forced update is an explicit
operator decision and can interrupt runs.

For an unattended maintenance window, schedule `nexusd update --install`
without `--force`; it is a no-op when current and refuses during active work.
Keep the hub and host protocol major versions compatible, and upgrade the hub
before the hosts.

The daemon also checks the release feed at startup and every six hours and
reports `update.status` to the hub. **Hosts** shows an update badge and the
installed/latest versions; **Hosts -> host -> Check for updates** triggers an
immediate signed check. This only reports availability: applying an update
remains a host CLI action. Set `NEXUS_UPDATE_CHANNEL=prerelease` on the host
service to include prereleases, and provide `NEXUS_GITHUB_TOKEN` there so the
background check can read the private release feed. If the feed is
unreachable, the host detail shows the check error instead of a false
"up to date".

## Upgrade


1. Back up `hub.db` and `secret.key` (see above).
2. Pull the new image / replace `nexus-hub` and restart. Migrations run
   automatically on start.
3. Replace `nexusd` on each host and restart the per-user service:
   `nexusd service install` (or restart the existing systemd user unit/launch
   agent/Task Scheduler task).
4. Verify `nexusd doctor`, `GET /healthz` and `GET /readyz`.

If a release changes the host-link protocol major version, update hosts before
or together with the hub; the hub refuses mismatched majors with an upgrade
hint.

## Windows background service

`nexusd service install` creates a **per-user Task Scheduler task** named
`AgentNexus Host`:

- trigger: at logon of the current user;
- restart on failure: up to three times, one minute apart;
- multiple instances: ignored;
- run level: least privilege (`InteractiveToken`), no administrator rights and
  no SYSTEM account;
- action: `nexusd.exe run`.

This runs in the same user profile as the coding agents and their existing
logins, which is required for native session resumption. It does **not** start
before the user logs in; a boot-time task would run in the wrong context. It was
tested on Windows 11 (10.0.26100) with a non-elevated account. Windows Server
2025 uses the same Task Scheduler schema but was not available for testing.

```powershell
nexusd service install
nexusd service status
nexusd service uninstall
```

The installer also removes the legacy `AgentNexus.vbs` Startup launcher if it
exists. If Task Scheduler is disabled by policy, installation fails with the
`schtasks` error instead of silently creating a less reliable autostart.

## Linux and macOS services

- Linux: a systemd **user** unit at
  `~/.config/systemd/user/agentnexus.service`, enabled with `systemctl --user`.
  Enable lingering (`loginctl enable-linger <user>`) if it must start before
  login.
- macOS: a LaunchAgent at
  `~/Library/LaunchAgents/dev.agentnexus.nexusd.plist` with `RunAtLoad` and
  `KeepAlive`.

Both need the same user context as the agents. `nexusd doctor` reports missing
or unreadable agent stores.

## Reverse proxy and TLS

Use Caddy, Traefik, Nginx Proxy Manager or nginx with WebSocket support. The
hub checks `Origin`, requires `X-Nexus-Request` on mutating requests and sets
`Secure` cookies when `NEXUS_PUBLIC_URL` starts with `https://` or
`--tls-cert` is used.

Example Caddy block:

```caddyfile
nexus.example.com {
    reverse_proxy hub:8080
}
```

Nginx Proxy Manager: enable **Websockets Support**, forward to the hub's HTTP
port, and request a Let's Encrypt certificate. Preserve the original `Host`
header (`proxy_set_header Host $host`) or set `NEXUS_PUBLIC_URL` so the hub
accepts the browser `Origin` behind a rewriting proxy. Do not terminate TLS at
the hub and the proxy at the same time unless both are configured consistently.

## First-run and recovery

- The first browser that completes `/api/v1/setup` becomes the `owner`.
- If the owner password is lost and TOTP is enabled, there is currently **no
  self-service recovery**. Stop the hub, back up `hub.db`, remove the user and
  session rows with a SQLite client, then complete setup again. This requires
  shell access and is not exposed over the API.
- Revoking a host closes its link and prevents reconnection. Pair it again with
  a new code.
- Revoking a device deletes its sessions and closes its browser WebSocket.

## Passkeys and step-up

Passkeys require a hostname and a secure browser context. `localhost` works for
development; `127.0.0.1` does not satisfy WebAuthn relying-party rules. Set:

```bash
nexus-hub --public-url https://nexus.example.com ...
```

Sensitive actions (new host pairing code, revoke host/device, user and grant
management, passkey/TOTP setup) require a step-up confirmation valid for five
minutes. A passkey is preferred; password + TOTP is the fallback. Changing the
public hostname invalidates existing passkey registrations.

## Host pairing

A signed-in user creates a one-time code under **Hosts → Pair a host**. The
daemon prints a short authentication string (SAS). Compare it with the SAS on
the Hosts page and confirm there before the host can connect. Codes are
single-use and expire after ten minutes. Anyone who obtains a code during its
lifetime can create a *pending* host, but cannot connect until the SAS is
confirmed in the hub.

Device (QR) pairing is separate: a signed-in device creates a two-minute link
that grants a new revocable browser session. Treat it like a temporary password.

## Structured mode

- **OpenCode:** a loopback-only `opencode serve` process per run, random port
  and server password, killed when the run stops.
- **Codex:** `codex app-server --stdio` per run; `thread/resume` for existing
  sessions, `thread/start` for new ones.
- **Claude Code / other ACP agents:** any configured ACP adapter
  (`nexusd acp --agent claude --command claude-agent-acp`). `claude-agent-acp`
  and `claude-code-acp` are auto-detected on `PATH`.
- Approvals are forwarded to the web UI and default-deny after ten minutes.
- Structured and terminal runs on the same native session are mutually
  exclusive per host; the daemon rejects a second run until the first exits.

If a daemon crashes, orphaned `opencode serve`/`codex app-server` processes may
survive. Inspect the host process list and terminate the orphan before resuming
that native session.

## Active runs, export and notifications

- **Runs** shows live runs across every host you can see, with state updates
  over the browser WebSocket.
- **Export MD / JSON** downloads the full transcript directly from the host
  (bounded at 10,000 messages per export). Export requires `read_transcript`.
- Web Push sends content-free notifications for approvals, run completion,
  failures and host disconnects. Requirements: HTTPS (localhost is treated as
  secure), browser notification permission and, on iOS, an installed PWA.
  The payload contains only a generic title, body and run URL.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Host stays offline | `nexusd status`, hub logs, outbound TLS/WebSocket path through the proxy |
| Pairing fails | code expired after 10 minutes, already used, host clock skew, or SAS not confirmed |
| No sessions discovered | `nexusd discover --json`; verify config-dir overrides and `nexusd doctor` |
| Resume rejected | cwd outside `allowed_roots`, agent not allowed, forbidden permission-bypass flag, or the session is already active |
| Transcript/export empty | host offline, unsupported agent version, or parser warning; check host logs |
| Terminal blank | run may still be starting; check run state, host logs, TTY size |
| Audit chain warning | restore from backup and investigate; the `audit` table was modified |
| Windows task not installed | `schtasks /Query /TN "AgentNexus Host"`; installation needs Task Scheduler enabled for the user |
| Docker permission error on `/data` | named volume UID mismatch; use the bundled image's volume ownership or `chown 65532` for bind mounts |
