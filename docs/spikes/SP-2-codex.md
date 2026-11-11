# SP-2: Codex CLI sessions

Status: verified against a real local store on Windows 11 in an earlier pass;
structured mode is verified against a scripted app-server peer, not every Codex
version.

## What exists

- Discovery reads `$CODEX_HOME` (default `~/.codex`) under `sessions/`.
  `rollout-*.jsonl` and `rollout-*.jsonl.zst` are supported; zstd is decoded
  with `klauspost/compress`.
- `session_meta` supplies the native id, cwd and timestamp. `response_item`
  lines supply user/assistant messages, `function_call` and
  `function_call_output`.
- Native resume is `codex resume <id>`; fork is `codex fork <id>`; new starts a
  fresh CLI session.
- Structured mode starts `codex app-server --stdio` and speaks JSON-RPC:
  `initialize`, `thread/start`, `thread/resume`, `turn/start`,
  `turn/interrupt`, `model/list` and approval replies (`session/requestApproval
  `-style requests).
- Model options come from `model/list`, filtered to non-hidden entries, with
  supported reasoning efforts as variants.

## Verified by tests

| Claim | Evidence |
| --- | --- |
| Rollout discovery, metadata, transcript and command building | `internal/host/adapter_test.go` `TestCodexAdapter` |
| Search over rollouts and UUID-based native ids | `internal/host/adapters_extra_test.go` `TestClaudeAndCodexSearch` |
| JSON-RPC request correlation, options, prompt, interrupt, approvals | `internal/host/structured_codex_test.go` `TestCodexRunJSONRPC` |
| Full structured start against a fake app-server | `internal/host/structured_start_test.go` `TestStartCodexStructuredAgainstFakeAppServer` |
| Malformed content does not panic | `internal/host/parsers_fuzz_test.go` `FuzzCodexContent`, `FuzzCodexRequestID` |

## Not verified

- Every app-server protocol revision. Approval request method names and
  decision values are matched tolerantly, but a new Codex release can change
  them.
- Interaction with the same thread open in a Codex TUI; the concurrency guard
  is AgentNexus-local plus a best-effort process scan.
- Linux and macOS stores; only Windows was exercised with a real store.
- `thread/list` coverage of exec-mode sessions and `sourceKinds` values.

## Fallbacks

- Terminal mode uses the native CLI and is the universal path.
- Unknown JSONL lines are skipped; a broken rollout file does not block other
  sessions.
- If structured startup fails, the host reports `run.error` and the user can
  resume in terminal mode.

## Tested version band

`codex >= 0.50.0 < 1.0.0`. Outside this band structured mode is disabled with a
warning; terminal mode remains available.
