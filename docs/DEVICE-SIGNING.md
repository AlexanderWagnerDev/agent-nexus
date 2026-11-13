# Client/device-signed commands: trust analysis

AgentNexus already signs every hub-to-host command with an Ed25519 hub key and
the host rejects replayed nonces. Browser-to-hub commands are authenticated by
an HttpOnly session cookie, per-device sessions and step-up for sensitive
actions. The remaining question is whether commands can also be signed by the
client (device) itself.

## Implemented: native client device keys

The hub now accepts a command-signing key per device:

- `PUT /api/v1/devices/{id}/key` registers an Ed25519 (32-byte) or P-256
  (uncompressed) public key. It requires step-up, so a stolen cookie cannot
  enrol an attacker-controlled key.
- `DELETE /api/v1/devices/{id}/key` removes it. Revoking the device removes the
  key through the foreign key.
- Once a device has a key, the hub requires a signature for prompt, approval,
  terminal input and interrupt messages on the client WebSocket, and for
  `POST /api/v1/runs/{id}/stop`.
- The signed payload is a newline-separated canonical string:
  - HTTP: `http`, method, path, SHA-256(body), Unix timestamp, nonce.
  - WebSocket: `ws`, message type, run id, SHA-256(text or approval id +
    decision or base64 input), Unix timestamp, nonce.
- Signatures are base64. P-256 accepts both IEEE P1363 (WebCrypto) and ASN.1.
- A per-device nonce cache plus a ±2 minute timestamp window rejects replays.
- Operators can set `--require-signed-commands` (or
  `NEXUS_REQUIRE_SIGNED_COMMANDS=true`) to require signatures from every
  device, including cookie-only browsers. The web UI does not hold a device
  key, so this mode is for native CLI/MCP clients.

The browser UI deliberately remains cookie-authenticated. It is delivered by
the hub, so a compromised hub can modify the JavaScript and sign any payload it
wants; a browser-held key would add no protection in that threat model. The
implemented key path protects native clients against stolen cookies and
replayed write requests.

## What the current web architecture cannot guarantee

The browser UI is delivered by the hub. If the hub is compromised, it can serve
modified JavaScript that exfiltrates a client-held key or signs a different
payload. A JavaScript-held device key therefore adds little against a
compromised hub; it mainly protects against stolen cookies and replayed
requests. A WebAuthn assertion is stronger because the private key stays in the
authenticator and the browser signs only the challenge the hub supplies, but a
compromised hub can still ask the authenticator for a valid assertion for a
different action unless the signed challenge binds the exact canonical request
and the user can inspect it. The current UI shows a confirmation, not a
hardware-displayed transaction.

## Honest conclusion

- Do not claim end-to-end client-signed commands for the web UI.
- Keep hub command signing, nonce replay protection, device sessions, step-up
  and host policy as the enforced properties.
- The implemented native key path is opt-in per device and covered by tests for
  Ed25519, P-256/P1363, replay rejection and unsigned-write rejection. It is
  hub-side enforcement; host-side rejection of hub-forged commands is a
  separate future step.
- If the hub itself is untrusted, the only safe design is a client that does
  not run hub-delivered code and verifies host identities independently. That
  is outside the current architecture.
