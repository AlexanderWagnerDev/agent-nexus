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
| Host updater state | `<config-dir>/AgentNexus/update-state.json` (metadata only, mode `0600`) |
| Host update log | `<config-dir>/AgentNexus/update.log` (Windows helper) |
| Host session cache | in memory; rebuilt from the native agent stores on restart |
| Hub audit log | `audit` table in `hub.db` (hash-chained) |

`<config-dir>` is `%APPDATA%` on Windows, `~/.config` on Linux and
`~/Library/Application Support` on macOS. Native agent sessions stay in the
agents' own stores; the hub never rewrites them.

## Docker image (GHCR and Docker Hub)

Development images are published to GitHub Container Registry and to
[Docker Hub](https://hub.docker.com/r/alexanderwagnerdev/agent-nexus) with the
same tags. There is no official version yet: `alpha` tracks `main`, and
`sha-<short>` pins a commit.

```bash
docker pull ghcr.io/alexanderwagnerdev/agent-nexus:alpha
docker pull ghcr.io/alexanderwagnerdev/agent-nexus:sha-167a829   # pinned build
docker pull alexanderwagnerdev/agent-nexus:alpha                 # Docker Hub
```

The Docker Hub push in `.github/workflows/docker.yml` needs two repository
secrets: `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` (a Docker Hub personal
access token with read/write scope).

The image is multi-arch (`linux/amd64`, `linux/arm64`, `linux/386`,
`linux/arm/v6`, `linux/arm/v7`, `linux/ppc64le`, `linux/s390x`,
`linux/riscv64`), runs as the nonroot user (UID 65532) on Alpine, writes only
to `/data`, and exposes a `/nexus-hub --healthcheck` probe.

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

The host updates itself without a hub-managed binary feed. Downloads come from
the official GitHub release repository configured locally; the hub never
selects or serves an executable.

```bash
nexusd update --status
nexusd update --check
nexusd update --stage
nexusd update --install          # refuses while agent runs are active
nexusd update --rollback
```

The daemon checks at startup and every six hours (configurable, with jitter,
ETag caching and exponential backoff) and reports `update.status` to the hub.
A hub reconnect wakes the check early. The check works while the hub is offline.
There is no release feed during alpha, so `--check` reports that no release
exists until the first approved version. A private test feed can be selected
with `NEXUS_UPDATE_API` and `NEXUS_UPDATE_REPO`; a private repository needs a
read-only `NEXUS_GITHUB_TOKEN` with Contents access.

### Channels and local settings

```bash
nexusd update --configure --channel stable
nexusd update --configure --channel prerelease
nexusd update --configure --channel development
nexusd update --configure --auto on
nexusd update --configure --install-when-idle on
nexusd update --configure --allow-remote-install off
nexusd update --configure --require-signature
nexusd update --configure --checksum-only
```

`stable` accepts only non-prerelease releases. `prerelease` also accepts
explicitly published prereleases. `development` uses the prerelease feed;
workflow artifacts are installed only through the installer. Settings are
stored in `nexusd.json` and are never changed by the hub. `NEXUS_UPDATE_CHANNEL`
is a process-level override; `NEXUS_AUTO_UPDATE=0` disables checking for the
current process.

### Verification and installation

- SHA-256 is always verified against `checksums.txt`.
- `updates.require_signature` defaults to `true`: a cosign keyless bundle for
  this repository's release workflow must verify. A missing/invalid signature
  is an error, never a silent downgrade. Checksum-only is an explicit local
  decision and is shown as a weaker mode in the UI.
- Download URLs are restricted to GitHub hosts for token forwarding, HTTPS
  downgrades are rejected, downloads are size-limited and only the expected
  binary is extracted (no path traversal or symlink following).
- The staged binary must report the expected `nexusd` version before it can
  replace a working installation.
- Installation requires the per-user service and waits until no terminal,
  structured or ACP run is active. New runs are rejected during replacement.
  `nexusd.previous` is kept; Windows keeps an update log at
  `%APPDATA%\AgentNexus\update.log`.
- A new version must publish its runtime status. Repeated restarts inside the
  verification window, or a failed Windows start check, restore
  `nexusd.previous`. The failed version is blocked after three failures; clear
  it with `nexusd update --configure --unblock <version>`.
- The host identity, config, native agent stores and credentials are preserved.

A forced update (`--force`) is an explicit operator decision and can interrupt
runs. Do not use it in an automatic maintenance task.

**Hosts** shows the installed and latest versions, channel, auto-update and
signature mode, last check, last success, update state and any error. A manager
can request installation from the host page only when `allow_remote_install` is
enabled locally; the host still applies its own channel, signature and idle
policy. Keep the hub and host protocol majors compatible and upgrade the hub
before the hosts.

## Upgrade


1. Back up `hub.db`, `secret.key` and `hub-signing.key` (see above).
2. Pull the new image / replace `nexus-hub` and restart. Migrations run
   automatically on start.
3. Hosts update themselves on the configured channel when idle. To update one
   immediately, run `nexusd update --install` (without `--force` while agents
   are active).
4. Verify `nexusd doctor`, `GET /healthz` and `GET /readyz`.

If a release changes the host-link protocol major version, update hosts before
or together with the hub; the hub refuses mismatched majors with an upgrade
hint. Older hosts are not silently given features they cannot execute.

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
  `~/.config/systemd/user/agentnexus.service`, enabled with `systemctl --user`
  and restarted after a crash. The unit carries a per-user `PATH` covering
  `~/.local/bin`, `~/.npm-global/bin`, `/usr/local/bin` and Homebrew so agent
  CLIs installed outside the system path are found. Enable lingering
  (`loginctl enable-linger <user>`) if it must start before login.
- macOS: a LaunchAgent at
  `~/Library/LaunchAgents/dev.agentnexus.nexusd.plist` with `RunAtLoad`,
  `KeepAlive`, the same per-user `PATH`, and logs under
  `~/Library/Logs/AgentNexus/`.

Both need the same user context as the agents. `nexusd doctor` reports missing
or unreadable agent stores. Grant Full Disk Access / Files and Folders to the
daemon process on macOS when projects live in protected directories.

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
  The payload contains only a generic title, body and run URL. Global push
  categories fall back to per-host overrides; changing either never suppresses
  the in-app inbox.

## Request tracing

Every HTTP response carries `X-Request-ID`; a client may send a bounded,
log-safe value (8-64 characters, letters/digits/`-_.:`) and the hub reuses it
otherwise it generates one. API request logs include `request_id`, method,
route and status. Host-link requests forward the hub request id as `trace_id`
and log both ids together, so a slow search or transcript read can be followed
from the browser request to the host.

## Host search index

Each daemon builds `search-index.db` in its config directory on first start
and refreshes it in the background. Search falls back to the slower adapter
scan until the index reports ready. `nexusd index status` shows the state and
session count; `nexusd index rebuild` rebuilds it after changing
`allowed_roots` or `allowed_agents`. The file can grow to roughly the size of
the indexed transcript text and can be deleted at any time while the daemon is
stopped.

## Concurrency limits

The hub can refuse new runs before they reach a host. All limits default to
`0` (unlimited) and are evaluated per new run:

| Flag | Environment | Scope |
| --- | --- | --- |
| `--max-runs-per-host` | `NEXUS_MAX_RUNS_PER_HOST` | live runs on one host |
| `--max-runs-per-user` | `NEXUS_MAX_RUNS_PER_USER` | live runs started by one user |
| `--max-runs-per-project` | `NEXUS_MAX_RUNS_PER_PROJECT` | live runs on one host/cwd pair |

A rejected start returns HTTP 429 with `reason: run_limit`; no run row or host
command is created. Existing runs are never stopped by a limit change. Prompt
queues are additionally capped at 100 pending items per run
(`reason: queue_limit`).

## Run error codes

Failed and exited runs store a stable code next to the original message:
`network`, `authentication`, `authorization`, `agent_unavailable`,
`unsupported_version`, `session_conflict`, `host_policy`, `agent_protocol`,
`process_crash`, `timeout`, `database`, `update` or `internal`. The classifier
only reads text the host or agent already reported; it never invents an error
for a successful run.

## Metrics

`/metrics` is disabled by default. Enable it on the hub container and set a long
random bearer token:

```dotenv
NEXUS_METRICS=true
NEXUS_METRICS_TOKEN=replace-with-a-long-random-value
```

Prometheus scrape configuration:
```yaml
scrape_configs:
  - job_name: agentnexus
    scheme: https
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials: replace-with-a-long-random-value
    static_configs:
      - targets: ["nexus.example.com"]
```

The endpoint is served by the same hub process, so proxy it like the web app
and do not expose it without the token. Metrics contain counts and route
patterns only; no session ids, search terms or transcript text are exported.
HTTP request logs use a request id and the matched route pattern, never the
query string.

## Session metadata and tags

Local titles and tags are per-user hub metadata (`session_overrides`,
`session_tags`). They never modify the agent's native store, and deleting a
session removes its local metadata. Filtering by tag and sorting by title
happen in SQLite; the UI keeps its filter state in the URL. The session list
uses API paging plus native `content-visibility` for offscreen project groups,
so a 10,000-session index remains responsive without rendering every row.

## Disabled hosts and active-elsewhere conflicts

Disabling a host is reversible and requires step-up confirmation. The hub asks
the daemon to stop active runs, then closes the link; reconnect attempts are
rejected until the host is enabled again. Renaming a host only changes the
hub-side display name and survives reconnects.

When a host reports that a native terminal has a session open, resuming returns
HTTP 409 with `reason: active_elsewhere`. The UI offers attach (for an existing
AgentNexus run), read-only transcript, fork where supported, or a forced resume
that requires step-up and is written to the audit chain. Detection is a
best-effort process scan, not a guarantee.

## Dependency licenses

CI runs `node scripts/check-licenses.mjs` against the Go module graph and the
browser lockfile. Allowed licenses are MIT, BSD-2-Clause, BSD-3-Clause,
Apache-2.0, ISC and 0BSD. MPL-2.0 is accepted only for packages listed with a
written reason in `scripts/license-exceptions.json`; currently `axe-core`
(development-only accessibility scanner) and `github.com/hashicorp/golang-lru/v2`
(a transitive dependency of `modernc.org/libc`). Adding a dependency outside
these rules fails CI.

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
