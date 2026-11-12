# Client/device-signed commands: trust analysis

AgentNexus already signs every hub-to-host command with an Ed25519 hub key and
the host rejects replayed nonces. Browser-to-hub commands are authenticated by
an HttpOnly session cookie, per-device sessions and step-up for sensitive
actions. The remaining question is whether commands can also be signed by the
client (device) itself.

## What is achievable

- **CLI/MCP clients:** a native client can hold a device key outside the
  browser and sign a canonical request. This is technically meaningful and
  would add a second factor for tooling. It is not implemented yet; the CLI
  currently uses a revocable device session cookie.
- **WebAuthn/passkeys:** a passkey assertion can sign a challenge with a
  hardware-backed credential. The hub already uses WebAuthn for login and
  step-up, so a step-up assertion is a real possession factor for the actions
  it protects.
- **Host-side policy:** remains the final authority. A compromised hub cannot
  bypass `allowed_roots`, `allowed_agents`, new/fork or force-resume policy.

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
- A future native CLI/MCP key is worthwhile; it should sign a canonical request
  including method, path, run id, body hash, timestamp and nonce, with a
  server-side nonce window and revocation. That work is not in this alpha.
- If the hub itself is untrusted, the only safe design is a client that does
  not run hub-delivered code and verifies host identities independently. That
  is outside the current architecture.
