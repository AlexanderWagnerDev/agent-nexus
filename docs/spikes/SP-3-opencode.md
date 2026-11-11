# SP-3: OpenCode sessions

Status: discovery and structured mode are covered by fixtures and a fake server;
a real OpenCode store and `serve` session were exercised on Windows 11 in an
earlier pass.

## What exists

- Discovery opens OpenCode's SQLite store read-only through
  `modernc.org/sqlite`. Both the v1 and v2 table layouts are detected.
- Session rows provide native id, title, cwd, timestamps and message counts;
  message/part rows provide text, reasoning, tool calls, tool output and
  permission requests.
- Native resume is `opencode --session <id>`; new sessions start the CLI
  without a session flag. OpenCode v2 has no non-interactive fork, so fork is
  rejected.
- Structured mode starts `opencode serve --hostname 127.0.0.1 --port <free>`,
  reads the printed password and URL, then uses the local HTTP API for
  session creation, prompts, model/agent selection, interrupts and permission
  replies. Events arrive through `/api/event`.
- Attachments are written to a bounded per-run directory with safe names and
  magic-byte checks before they are referenced in a prompt.

## Verified by tests

| Claim | Evidence |
| --- | --- |
| v1/v2 discovery, transcript and command building | `internal/host/opencode_store_test.go`, `internal/host/opencode_test.go` |
| Model/mode options and prompt/interrupt requests | `internal/host/structured_open_test.go` |
| Event handling, approvals and stop | `internal/host/structured_open_test.go`, `internal/host/structured_acp_test.go` |
| Full structured start against a fake server | `internal/host/structured_start_test.go` `TestStartStructuredRunAgainstFakeOpenCode` |
| Attachment limits and cleanup | `internal/host/attachments_test.go` |
| Malformed tool parts do not panic | `internal/host/parsers_fuzz_test.go` `FuzzOpenCodeSnippet` |

## Not verified

- Cross-project session listing semantics in every OpenCode release.
- Whether all permission events appear in the SSE stream for every tool kind.
- The v1-to-v2 migration on a large real store; the adapter reads both layouts
  but does not migrate anything.
- Linux and macOS with a real store.

## Fallbacks

- Terminal mode resumes the native session without the local server.
- If the server does not report its password and URL within 25 seconds, the
  run fails cleanly and the session stays usable in the CLI.
- The index/search path is independent of structured mode; transcripts are read
  directly from SQLite.

## Tested version band

`opencode >= 1.0.0 < 2.0.0`. Outside this band structured mode is disabled
with a warning; terminal mode remains available.
