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
NEXUS_TAG=v0.3.1-rc.1
```

Point the DNS record for `HUB_DOMAIN` at the server, open TCP 80/443, then:

```bash
docker compose up -d
docker compose ps
```

Caddy obtains a TLS certificate automatically and proxies WebSockets to the
hub. The hub itself is not published on a host port.

### Portainer (external reverse proxy)

Use [deploy/portainer-stack.yml](../deploy/portainer-stack.yml) if another proxy
such as Nginx Proxy Manager terminates TLS:

1. Portainer → Stacks → Add stack.
2. Paste the file.
3. Set `HUB_DOMAIN`, `HUB_PORT` and `NEXUS_TAG`.
4. Deploy, then point the proxy at `http://<docker-host>:8787` with **WebSockets
   Support** enabled.

If you use a bind mount for `/data`, make it writable by UID `65532`:

```bash
sudo chown -R 65532:65532 /srv/agentnexus
```

The central database is `hub.db` in the `nexus-data` volume. See
[OPERATIONS.md](OPERATIONS.md) for backup and restore.

## 2. Create the owner account

Open `https://nexus.example.com` and complete first-run setup. Save the TOTP
secret if you want a second factor. Register a passkey under **Settings →
Devices & passkeys** after signing in; passkeys require the public hostname (an
IP address is not a valid WebAuthn origin).

## 3. Install and pair a host client

Download the archive for the machine from the latest GitHub release:

- Windows x64: `agent-nexus_<version>_windows_amd64.zip`
- Windows ARM64: `agent-nexus_<version>_windows_arm64.zip`
- Linux x64/ARM64: `agent-nexus_<version>_linux_amd64.tar.gz` or `linux_arm64`
- macOS Intel/Apple Silicon: `agent-nexus_<version>_darwin_amd64.tar.gz` or
  `darwin_arm64`

The archive contains `nexus-hub`, `nexusd`, checksums and SBOM files. Hosts only
need `nexusd`.

### Windows (including Windows Server 2025)

```powershell
Expand-Archive agent-nexus_*_windows_amd64.zip -DestinationPath C:\AgentNexus
cd C:\AgentNexus
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
2. Change `NEXUS_TAG` to the new release and run `docker compose up -d`.
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
