# Three Windows hosts with one hub

This is the concrete deployment for the primary scenario: one self-hosted hub
in Portainer, three Windows machines running `nexusd` and OpenCode, accessed
from a MacBook or phone browser. Hosts connect outbound; no inbound host ports,
SSH or WSL are required.

## 1. Hub (Portainer)

1. Create the hub network in Portainer or Docker:
   - use `deploy/portainer-stack.yml` when the reverse proxy is on the same
     host and reaches `127.0.0.1:8787`;
   - use `deploy/portainer-stack-proxy.yml` when Nginx Proxy Manager already
     runs in a Docker network. Set `PROXY_NETWORK` to that exact network name.
2. Set at least:

   ```dotenv
   HUB_DOMAIN=nexus.example.com
   NEXUS_TAG=alpha
   WUD_WATCH_DIGEST=true
   HUB_BIND=127.0.0.1
   HUB_PORT=8787
   ```

3. Deploy. In Nginx Proxy Manager create a Proxy Host:
   - Forward hostname: `agentnexus-hub` (proxy-network variant) or the Docker
     host address (port variant).
   - Forward port: `8080` (proxy variant) or `8787` (port variant).
   - Enable **WebSockets Support**, request a Let's Encrypt certificate and
     force SSL.
4. Open `https://nexus.example.com`, create the owner, register a passkey and
   enable TOTP.
5. Back up `hub.db`, `secret.key` and `hub-signing.key` from the
   `agentnexus-data` volume. Use **Hosts → Pair a host** for each machine.
   Disable or rename a host from the same page; disabling stops active runs and
   blocks new ones until it is enabled again, while sessions stay visible.

The IPv6 bridge requires IPv6 enabled in the Docker daemon. If your engine does
not support it, remove the IPv6 `ipam` entry and set `enable_ipv6: false`.

## 2. Each Windows host

Run in an elevated-free PowerShell as the **same user** that uses OpenCode.
The per-user service inherits that profile, so OpenCode logins, models, skills
and MCP config are available.

```powershell
# 1. Install or update the daemon from an authenticated development build.
#    The installer verifies the artifact checksum and executable, refuses while
#    runs are active, and only interrupts them after -Force plus typing FORCE.
#    No Go toolchain and no release archive are required.
gh repo clone AlexanderWagnerDev/agent-nexus C:\AgentNexus
powershell -ExecutionPolicy Bypass -File C:\AgentNexus\scripts\install-host.ps1 `
  -Hub https://nexus.example.com -Code XXXX-XXXX -Name win-1

# 2. Compare the printed SAS in Hosts and confirm it there.
#    The script installs the per-user Task Scheduler service and runs doctor.
```

Repeat with `--name win-2` and `--name win-3`.

Do not open firewall ports on the hosts. `nexusd` only dials out to the hub.

### Optional update token

The release repository is private. For update checks, create a fine-grained
GitHub token with **Contents: read** and set it in the user environment before
starting the task:

```powershell
setx NEXUS_GITHUB_TOKEN "github_pat_..."
```

Restart the task once (`schtasks /End /TN "AgentNexus Host"` and `/Run`) so the
new environment is visible. Without the token, pairing and normal operation
work; only update checks are unavailable.

Install cosign on hosts that should verify the signed checksum bundle
automatically. Without cosign, updates require
`nexusd update --install --allow-checksum-only` and are an explicit decision.

## 3. Native OpenCode sessions

- Sessions are discovered from each host's own OpenCode store within a few
  seconds (mtime fingerprint, 500 ms debounce, 15 s safety rescan). New sessions
  created in a native terminal appear automatically.
- Open **Sessions** and filter by project, host or agent. Identical repository
  remotes are grouped across hosts; worktrees are counted separately.
- Open an existing session in **Structured chat** to continue the same native
  session. The session ID, working directory and agent configuration stay
  native; the original CLI can reopen it afterwards.
- A session already running in a native terminal is marked **Open in native terminal**.
  The dashboard asks before a forced resume: attach to an existing AgentNexus
  run, read the transcript without writing, fork where the agent supports it,
  or confirm a step-up-protected resume. Forced resumes are audited.
- The hub prevents two AgentNexus runs for one native session and keeps a
  controller lease per browser device.

## 4. Verify the three-host setup

1. **Hosts** shows all three as online with the expected daemon version.
2. **Sessions** shows sessions from all three even when titles repeat; the
   project path and host distinguish them.
3. Open two different sessions on two hosts in **Split workspace**. Both should
   stream independently and keep separate drafts and approvals.
4. Start a prompt on a newly discovered session and check that the native
   OpenCode CLI can reopen the same session afterwards.
5. Open two different sessions on two hosts in **Split workspace**. Both should
   stream independently and keep separate drafts and approvals.
6. Start a prompt on a newly discovered session and check that the native
   OpenCode CLI can reopen the same session afterwards.
7. Disable one host in **Hosts**. Its active runs stop and new runs are refused;
   the sessions remain visible. Enable it again and watch it reconnect.
8. Disconnect one host (stop the task, not the machine). The hub marks it
   offline; other hosts stay usable. Restart the task and watch it reconnect.
9. Restart the hub container. Browsers show a reconnect and the run state
   resynchronizes; hosts reconnect automatically. Terminal output produced
   during the outage is replayed from each host's 1 MiB run buffer.

## 5. Updates and rollback

On a host, when no runs are active:

```powershell
cd C:\AgentNexus
.\nexusd.exe update --check --channel prerelease
.\nexusd.exe update --install
```

The updater verifies the archive checksum and, when cosign is installed, the
signed bundle; it preserves the host identity and keeps
`nexusd.exe.previous`. If the update fails, the Windows helper rolls back
automatically and logs to `%APPDATA%\AgentNexus\update.log`. Manual rollback:

```powershell
.\nexusd.exe update --rollback
```

Run these from an OS maintenance task if desired; without `--force` the updater
refuses while active runs are reported.

## 6. Troubleshooting

| Symptom | Check |
| --- | --- |
| Host offline | `schtasks /Query /TN "AgentNexus Host" /V /FO LIST`, `nexusd doctor`, hub URL and TLS certificate |
| No sessions | OpenCode store exists in the host user profile; `nexusd doctor` shows `opencode` detected; allowed roots include the project path |
| Update check fails | `NEXUS_GITHUB_TOKEN`, repository access, clock, and `nexusd update --check --channel prerelease` |
| Signature rejected after upgrade | The host must re-pin the hub key: remove `hub_public_key` from `nexusd.json` and reconnect, or pair again; never silently fall back to unsigned commands |
| WebSocket/proxy errors | WebSockets enabled, `NEXUS_PUBLIC_URL` matches the external HTTPS hostname |
| Hub data loss | Restore `hub.db`, `secret.key` and `hub-signing.key` together; see OPERATIONS.md |
