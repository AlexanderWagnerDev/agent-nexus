# `nexus` terminal client

`nexus` is a small cross-platform client for a self-hosted hub. It is built
from `cmd/nexus` and has no extra dependencies.

```sh
nexus login --hub https://nexus.example.com --token <device-pairing-token>
nexus sessions
nexus attach <run-id>
nexus approve <run-id> <approval-id> once
nexus logout
```

Create the pairing token in the web app under **Devices & passkeys**. Password
login is also supported with `--username` and `NEXUS_PASSWORD` (plus `--totp`
when enabled); prefer the pairing token because the password never appears in
the process list.

The session cookie is stored in the per-user config directory
(`AgentNexus/cli.json`, mode 0600 on Unix) and is revoked by `nexus logout` or
from the Devices page. `attach` acquires the controller lease, subscribes over
the browser WebSocket, prints terminal output or structured text and sends
typed lines as input. It is line-oriented, not a raw PTY, so full-screen TUI
applications should use the web terminal. `approve` sends one decision after
acquiring the lease.

Tests: build plus a real local hub login/session/logout pass on Windows. The
attach/approve paths compile and use the same protocol as the browser client,
but were not exercised against a live host in this run.
