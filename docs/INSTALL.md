# Install AgentNexus

This guide gets a first usable installation running without building from
source. You need a Linux server (or any Docker host) for the hub and one or more
machines with coding agents installed.

## 1. Run the hub with Docker Compose

The repository is public during alpha. Clone it with Git or the GitHub CLI.
Authentication is not required to read or clone the source repository.

```bash
gh repo clone AlexanderWagnerDev/agent-nexus
cd agent-nexus
cp .env.example .env
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
tracks `main` and `sha-<short>` pins a commit. Digest watching is enabled by
default so WUD notices new `alpha` builds; set `WUD_WATCH_DIGEST=false` when you
pin a `sha-` tag or a fixed version tag. The default `WUD_TAG_INCLUDE` watches
`alpha` and immutable semver tags, not `sha-` pins. After an approved release,
set `NEXUS_TAG=v0.1.0` (or `0.1.0`) and keep digest watching off for that
immutable tag.

WUD only reports updates; it does not apply them. This is separate from the
host updater: WUD watches the hub container, while `nexusd` updates itself on
each Windows/Linux/macOS host. Never let a hub container update cross a schema
change without a backup. A deliberate, opt-in hub update strategy is to watch
only the stable version tag, take a backup, and then pull the new tag manually;
no automatic database migration without a backup is provided.

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

The installer scripts support `windows/amd64`, `windows/arm64`, `linux/amd64`,
`linux/arm64`, `darwin/amd64` and `darwin/arm64`. Official GitHub releases are
the default source: the archive contains `nexusd` and the `nexus` terminal
client. The script verifies the archive SHA-256, verifies the cosign bundle when
`cosign` is installed, checks the downloaded binary's version, preserves an
existing host identity and installs the per-user service. No GitHub CLI and no
Go toolchain are required for a public release.

During alpha there is no public release yet. The development workflow builds
remain available as an explicit fallback (`-Development` / `--development`);
that path uses the GitHub CLI and authenticated Actions artifacts and should
not be mixed with release installations on the same host.

Clone the repository once on the machine that runs the installer so the script
is available locally:

```bash
git clone https://github.com/AlexanderWagnerDev/agent-nexus
```

If you prefer a source build, `go build -trimpath -o nexusd ./cmd/nexusd`
still works. Go 1.26.11 or newer is required.

### Windows (including Windows Server 2025)

```powershell
git clone https://github.com/AlexanderWagnerDev/agent-nexus C:\AgentNexus
powershell -ExecutionPolicy Bypass -File C:\AgentNexus\scripts\install-host.ps1 `
  -Hub https://nexus.example.com -Code XXXX-XXXX -Name my-laptop
```

Official release (after the first approved version):

```powershell
powershell -ExecutionPolicy Bypass -File C:\AgentNexus\scripts\install-host.ps1 `
  -Hub https://nexus.example.com -Name my-laptop -Channel stable
```

During alpha, add `-Development` to install the newest authenticated workflow
build. Upgrades run without `-Code`; an existing pairing is kept. The daemon
prints a **Pairing SAS** (for example `123-456`) on first pairing. In the hub,
open **Hosts**, compare the SAS and click **Confirm host**.

The installer also installs and starts the per-user background task. Check it
with:

```powershell
& "$env:LOCALAPPDATA\AgentNexus\nexusd.exe" service status
```

The task starts at logon of the current user, runs with least privilege, ignores
duplicate instances and restarts up to three times after a crash. It uses the
same Windows profile as your coding agents, so their logins, models, skills and
MCP configuration are available. It needs no administrator rights and no WSL.

Uninstall the binaries while keeping the host config and pairing:

```powershell
powershell -ExecutionPolicy Bypass -File C:\AgentNexus\scripts\install-host.ps1 -Uninstall
```

Add `-PurgeConfig` only when the AgentNexus host config should be removed too.
Native agent data is never touched.

### Linux

```bash
git clone https://github.com/AlexanderWagnerDev/agent-nexus
./agent-nexus/scripts/install-host.sh --hub https://nexus.example.com --code XXXX-XXXX --name build-box
```

During alpha: `--development`. Upgrades omit `--code`.

The script installs `~/.local/bin/nexusd` and `~/.local/bin/nexus` and writes
the systemd **user** unit to `~/.config/systemd/user/agentnexus.service`. To run
it without an interactive login, enable lingering once:

```bash
loginctl enable-linger "$USER"
```

Uninstall with `./scripts/install-host.sh --uninstall` (add
`--purge-config` to remove the host config as well) or
`nexusd service uninstall`.

### macOS

```bash
git clone https://github.com/AlexanderWagnerDev/agent-nexus
./agent-nexus/scripts/install-host.sh --hub https://nexus.example.com --code XXXX-XXXX --name macbook
```

During alpha: `--development`. The LaunchAgent starts at login and is kept
alive. Grant Full Disk Access / Files and Folders permission if your projects
live under `~/Documents`, `~/Desktop` or iCloud Drive. Downloaded binaries are
not notarized in the alpha; if Gatekeeper blocks one, inspect and remove the
quarantine attribute explicitly (`xattr -d com.apple.quarantine <path>`) only
after verifying the release checksum and cosign bundle.

## 3.1 Host updates

The host updates itself; the hub never serves or selects a binary. The trusted
source is the official GitHub release feed configured locally on the host.

Default policy for an official installation:

```json
{
  "updates": {
    "channel": "stable",
    "check_interval_minutes": 360,
    "install_when_idle": true,
    "require_signature": true,
    "allow_remote_install": false
  }
}
```

- `stable` accepts only non-prerelease releases. `prerelease` also accepts
  explicitly published prereleases. `development` uses the prerelease feed;
  development workflow artifacts are installed only through the installer.
- Checks run at startup and about every six hours (configurable, 15 minutes to
  seven days) with jitter, ETag caching and exponential backoff on errors. A
  successful hub reconnect triggers an early check; the hub is not required for
  update detection.
- Downloads are limited to GitHub; redirects may not downgrade HTTPS and the
  GitHub token is only sent to GitHub hosts. Archives are size-bounded and only
  the expected binary is extracted (no path traversal or symlink following).
- SHA-256 is always verified. With the default `require_signature: true`, a
  valid cosign keyless bundle for this repository's release workflow is
  required; a missing or invalid signature never falls back silently.
  Checksum-only is an explicit weaker mode:

  ```bash
  nexusd update --configure --checksum-only
  ```

- Installation waits until no terminal, structured or ACP run is active and
  new runs are rejected during the replacement. It requires the per-user
  service. `nexusd.previous` is kept for rollback; repeated failed starts roll
  back automatically and block that version until an operator clears it.
- The host identity, `nexusd.json`, native agent stores and credentials are not
  touched.

Commands:

```bash
nexusd update --status
nexusd update --check
nexusd update --stage
nexusd update --install          # refuses while active runs are reported
nexusd update --install --force  # explicit operator override
nexusd update --rollback         # restore nexusd.previous
nexusd update --configure --channel stable --auto on --install-when-idle on
nexusd update --configure --require-signature
nexusd update --configure --allow-remote-install on
nexusd update --configure --unblock 0.1.1
```

Automatic installation can be disabled locally with
`nexusd update --configure --auto off`. A private mirror or test feed can be
selected with `NEXUS_UPDATE_API` and `NEXUS_UPDATE_REPO`; a private repository
needs a read-only `NEXUS_GITHUB_TOKEN` with **Contents: read**. `NEXUS_AUTO_UPDATE=0`
is an emergency environment override for the current process.

The hub shows the host version, available update, channel, auto-update and
signature mode, last check, last success, current state and the last error. A
manager may request installation from the host page, but the host performs it
only when `allow_remote_install` is enabled locally. A blocked version must be
unblocked on the host.

Windows update logs are written to `%APPDATA%\AgentNexus\update.log`. The
detached helper stops the Task Scheduler task, replaces the executable, starts
the task and checks that the expected version reports from `runtime.json`; it
restores the previous binary when that check fails. Linux uses the systemd user
unit and macOS the LaunchAgent.

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

1. Back up `hub.db`, `secret.key` and `hub-signing.key`
   (see [OPERATIONS.md](OPERATIONS.md)).
2. Change `NEXUS_TAG` to the new image tag, run `docker compose pull`, then
   `docker compose up -d`. The default compose file only pulls; add
   `-f compose.build.yaml` only if you intentionally build from a checkout.
3. Host daemons update themselves on the configured channel when idle. Check
   `nexusd update --status` and `nexusd doctor` afterwards. To update a specific
   host immediately, run `nexusd update --install`; it refuses while agent runs
   are active unless `--force` is used after a deliberate check.
4. Re-running the installer is an alternative and also verifies the release
   archive. It keeps `nexusd.previous`, preserves the host identity and restores
   the previous binary if service installation fails.
5. Check `/healthz` and `/readyz` on the hub.

Upgrade the hub before hosts when a release changes the host protocol. New
hosts negotiate capabilities, so older hosts keep working with features they
support; the hub shows unsupported capabilities instead of silently enabling
them.

## Uninstalling a host

```powershell
# Windows
& "$env:LOCALAPPDATA\AgentNexus\nexusd.exe" service uninstall
```

```bash
# Linux and macOS
nexusd service uninstall
```

Then revoke the host on the **Hosts** page. Revoking fails active runs and
requires a new pairing code for that machine.

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
