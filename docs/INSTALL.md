# Install AgentNexus

This guide gets a first usable installation running without building from
source. You need a Linux server (or any Docker host) for the hub and one or more
machines with coding agents installed.

## 1. Run the hub with Docker Compose

Create a directory and copy the bundled files:

```bash
mkdir agentnexus && cd agentnexus
curl -LO https://raw.githubusercontent.com/AlexanderWagnerDev/agent-nexus/main/compose.yaml
curl -LO https://raw.githubusercontent.com/AlexanderWagnerDev/agent-nexus/main/.env.example
curl --create-dirs -LO https://raw.githubusercontent.com/AlexanderWagnerDev/agent-nexus/main/deploy/Caddyfile
mv .env.example .env
```

Edit `.env`:

```dotenv
HUB_DOMAIN=nexus.example.com
HUB_NAME=AgentNexus
NEXUS_TAG=alpha
```

Point the DNS record for `HUB_DOMAIN` at the server, open TCP 80/443, then:

```bash
docker compose up -d
docker compose ps
```

Caddy obtains a TLS certificate automatically and proxies WebSockets to the
hub. The hub itself is not published on a host port.

### Portainer (external reverse proxy)

Two complete stacks are provided:

- [deploy/portainer-stack.yml](../deploy/portainer-stack.yml) publishes a host
  port (loopback by default) for a proxy on the same host.
- [deploy/portainer-stack-proxy.yml](../deploy/portainer-stack-proxy.yml)
  attaches the hub to an existing external proxy network and publishes no host
  port. Set `PROXY_NETWORK` to the exact existing network name, for example
  `npm_proxy`.

1. Portainer -> Stacks -> Add stack.
2. Paste the matching file.
3. Set `HUB_DOMAIN`, `NEXUS_TAG` and, for the proxy variant, `PROXY_NETWORK`.
4. Deploy. For the port variant, point the proxy at
   `http://<docker-host>:8787` with **WebSockets Support** enabled. For the
   proxy variant, use `http://agentnexus-hub:8080`.
5. Keep `HUB_BIND=127.0.0.1` unless the proxy runs on another machine and a
   firewall protects the port.

Both stacks use `restart: always`, a named `agentnexus-data` volume, a
read-only root filesystem with a small `/tmp`, no capabilities, and an IPv6
bridge (`agentnexus-net`). IPv6 requires the Docker daemon to have IPv6
enabled; adjust or remove the IPv6 `ipam` entry if your engine does not.

The stack includes WUD labels. There is no official release yet: `alpha`
tracks `main` and `sha-<short>` pins a commit. Set `WUD_WATCH_DIGEST=true` only
when you deliberately follow the moving `alpha` tag. WUD only reports updates;
it does not apply them. Never let an update cross a hub schema change without a
backup.

If you use a bind mount for `/data`, make it writable by UID `65532`:

```bash
sudo chown -R 65532:65532 /srv/agentnexus
```

Back up `hub.db`, `secret.key` and `hub-signing.key` together; the database
alone cannot decrypt secrets.

The central database is `hub.db` in the `nexus-data` volume. See
[OPERATIONS.md](OPERATIONS.md) for backup and restore.

## 2. Create the owner account

Open `https://nexus.example.com` and complete first-run setup. Save the TOTP
secret if you want a second factor. Register a passkey under **Settings →
Devices & passkeys** after signing in; passkeys require the public hostname (an
IP address is not a valid WebAuthn origin).

## 3. Install and pair a host client

Build the host daemon from `main`. There are no downloadable release archives
during alpha.

```bash
go build -trimpath -o nexusd ./cmd/nexusd
```

CI cross-compiles the daemon for `windows/amd64`, `windows/arm64`,
`linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64`. Build on the
target machine or cross-compile with `GOOS`/`GOARCH`.

### Windows (including Windows Server 2025)

```powershell
go build -trimpath -o nexusd.exe ./cmd/nexusd
.\nexusd.exe pair --hub https://nexus.example.com --code XXXX-XXXX --name my-laptop
```

The daemon prints a **Pairing SAS** (for example `123-456`). In the hub, open
**Hosts**, compare the SAS and click **Confirm host**. The host stays blocked
until this confirmation.

Install the per-user background task:

```powershell
.\nexusd.exe service install
.\nexusd.exe service status
```

The task starts at logon of the current user, runs with least privilege, ignores
duplicate instances and restarts up to three times after a crash. It uses the
same Windows profile as your coding agents, so their logins, models, skills and
MCP configuration are available. It needs no administrator rights and no WSL.

To remove it:

```powershell
.\nexusd.exe service uninstall
```

### Linux

```bash
tar xzf agent-nexus_*_linux_amd64.tar.gz
cd agent-nexus_*
./nexusd pair --hub https://nexus.example.com --code XXXX-XXXX --name build-box
./nexusd service install
./nexusd service status
```

The systemd **user** unit is written to
`~/.config/systemd/user/agentnexus.service`. To run it without an interactive
login, enable lingering once:

```bash
loginctl enable-linger "$USER"
```

Uninstall with `./nexusd service uninstall`.

### macOS

```bash
tar xzf agent-nexus_*_darwin_arm64.tar.gz
cd agent-nexus_*
./nexusd pair --hub https://nexus.example.com --code XXXX-XXXX --name macbook
./nexusd service install
./nexusd service status
```

The LaunchAgent starts at login and is kept alive. Grant Full Disk Access /
Files and Folders permission if your projects live under `~/Documents`,
`~/Desktop` or iCloud Drive.

## 3.1 Host updates

`nexusd` can check and stage releases without any hub involvement. This keeps a
compromised hub out of the binary trust path.

During alpha there is no public release feed, so `update --check` reports that no
release exists. The staging, verification, rollback and active-run guards remain
implemented and are exercised against local release fixtures in the test suite.
A private test feed can be pointed at with `NEXUS_UPDATE_API` and
`NEXUS_UPDATE_REPO`.

```bash
nexusd update --check --channel prerelease
nexusd update --stage --channel prerelease
nexusd update --install          # refuses while active runs are reported
nexusd update --install --force  # explicit override
nexusd update --rollback         # restore nexusd.previous
```

- A private release feed needs a fine-grained read-only GitHub token with
  **Contents: read**, exported as `NEXUS_GITHUB_TOKEN` on the host.
- Every archive is verified against the release `checksums.txt` (SHA-256).
  If `cosign` is installed, the signed checksum bundle is also verified against
  the release workflow identity. `--allow-checksum-only` is the explicit
  fallback for hosts without cosign.
- `--install` requires the per-user service and keeps `nexusd.previous` for
  rollback. It reads `runtime.json` and refuses to restart while active runs
  are reported; `--force` is an explicit override.
- Windows update logs are written to `%APPDATA%\AgentNexus\update.log`; the
  helper stops and restarts the Task Scheduler task. Linux uses the systemd
  user unit and macOS the LaunchAgent.
- The host identity, config, native agent stores and credentials are not
  touched.

A safe automatic maintenance window can run `nexusd update --install` from the
OS scheduler. It will no-op when up to date and refuse while runs are active.
On Windows create a weekly task; on Linux/macOS use a systemd user timer or a
LaunchAgent. Set `NEXUS_GITHUB_TOKEN` in that task's environment. Do not enable
`--force` in an automatic task.

## 4. Start using existing chats

The host connects outbound only; no ports are opened on the machine. Within 30
seconds the hub discovers sessions from the agents' local stores.

1. **Sessions** — browse and search discovered chats.
2. **Structured chat** — open an OpenCode, Codex or Claude/ACP session and
   continue it with streaming, tools and approvals.
3. **Resume terminal** — open the original native TUI in a browser terminal.
4. **New session** — choose an agent directory and start a new native session.

Session IDs are the agents' native IDs; AgentNexus never imports a chat into a
new session. The original CLI can reopen the same session afterwards with
`opencode --session <id>`, `codex resume <id>` or `claude --resume <id>`.

## 5. Optional: Claude structured mode

Claude Code works in terminal mode out of the box. For the structured chat UI,
install an ACP adapter on the host and tell `nexusd` about it:

```bash
nexusd acp --agent claude --command claude-agent-acp
```

Restart the service. `claude-agent-acp` and `claude-code-acp` are detected on
`PATH` automatically. The adapter was not exercised against a real Claude
installation in this release; terminal mode is the verified fallback.

## 6. First checks

```bash
nexusd doctor
curl https://nexus.example.com/healthz
curl https://nexus.example.com/readyz
```

In the browser, verify **Overview**, **Hosts**, **Runs**, **Settings → Theme**
and a chat. If the host stays offline, check the hub logs and the outbound
WebSocket path through the proxy.

## Updating

1. Back up `hub.db` (see [OPERATIONS.md](OPERATIONS.md)).
2. Change `NEXUS_TAG` to the new image tag and run `docker compose up -d`.
3. Replace `nexusd` on each host and run `nexusd service install` again.
4. Check `nexusd doctor`, `/healthz` and `/readyz`.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| Passkey registration fails | Use the HTTPS hostname, not an IP address |
| Host is pending forever | Compare the SAS printed by `nexusd pair` with the Hosts page and confirm it |
| Host stays offline | Check `nexusd status`, outbound WSS, proxy WebSocket support and the Host header |
| No sessions found | Run `nexusd discover --json`; check that the agent CLI is installed and logged in |
| Resume rejected | Path outside `allowed_roots`, agent not in `allowed_agents`, or the session is already open |
| Chat has no old messages | The structured run was new (no hub session yet); open the discovered session or use the transcript view |
| Docker `/data` permission error | Named volume ownership is handled by the image; for bind mounts run `chown 65532:65532` |

More detail: [OPERATIONS.md](OPERATIONS.md) and [SECURITY.md](SECURITY.md).
