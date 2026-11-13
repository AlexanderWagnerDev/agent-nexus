# SP-4: Host link protocol

Status: verified by unit, integration and fuzz tests. Protobuf conformance and
long-haul network behavior are not verified.

## What exists

- JSON envelopes (`internal/protocol`) over a WebSocket. The host always dials
  out; there are no inbound host ports.
- Host authentication: `hello` with a nonce, Ed25519 signature over the nonce,
  `auth.ok` with a protocol version and feature list. A host must be confirmed
  with a SAS in the hub first.
- Host-to-hub commands are signed with the hub key when a signer is configured;
  hosts pin the key on first use. Replay protection uses nonces plus a
  five-minute timestamp window.
- A major-version mismatch is rejected with an actionable message.
- Session snapshots are full on connect and delta afterward
  (`session-delta-v1`); a reconnect repairs with a full snapshot.
- Client WebSocket: subscriptions, replay from a sequence number, terminal
  input, prompts, approvals, interrupts, stop, resize and lease messages.
- Hub restart: a new epoch is advertised; browsers reconnect with jitter and
  replay buffered events. Active runs are adopted from the host's
  `active_runs` report.
- Limits: host and browser read limits, prompt/terminal-input caps, 1 MiB HTTP
  bodies, bounded per-run output and event buffers.

## Verified by tests

| Claim | Evidence |
| --- | --- |
| Envelope signing, timestamp window and nonce replay | `internal/protocol/envelope_signature_test.go`, `internal/host/hubsig_test.go` |
| Host challenge/replay and confirmation gate | `internal/hub/websocket_test.go` |
| Scripted host link with sessions, search, runs and disconnects | `internal/hub/fakehost_test.go` |
| Real daemon against a fake hub, including policy rejection | `internal/host/daemon_link_test.go` |
| Oversized HTTP body and client WebSocket frame rejection | `internal/hub/api_test.go`, `internal/hub/websocket_test.go` |
| Arbitrary envelope bytes do not panic | `internal/protocol/envelope_fuzz_test.go` |

## Not verified

- Protobuf wire compatibility. The alpha uses JSON (ADR-014).
- Protocol behavior across version skew beyond the major-version check.
- Long-haul links with proxies, aggressive idle timeouts or high packet loss.
  The 25-second ping and reconnect backoff are implemented.
- Protection against a compromised hub that serves modified browser JavaScript;
  device-signed commands (B3) are not implemented.

## Fallbacks

- Sessions and transcripts remain in the agents' native stores; a failed link
  degrades to the native CLI.
- A run that loses the hub keeps running; output during the outage is not
  replayed, and the native store remains authoritative.
- Full snapshots after reconnect repair any missed delta.
