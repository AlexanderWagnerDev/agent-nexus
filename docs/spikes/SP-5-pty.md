# SP-5: PTY / ConPTY terminal mode

Status: implemented and tested for process lifecycle and policy; visual
terminal behavior under real agents is only manually verified on Windows 11.

## What exists

- `github.com/aymanbagabas/go-pty v0.2.3` provides the PTY on Unix and ConPTY
  on Windows. The hub streams output over the browser WebSocket to xterm.js and
  forwards keystrokes back.
- Terminal runs support resume, new and fork where the adapter exposes it.
  Arguments are passed as an argv array; a shell is never used except for the
  Windows `.cmd`/`.bat` shim.
- Resize messages are clamped (minimum 20x5) and forwarded to the PTY.
- Stop sends Ctrl-C first, waits 1.5 seconds for a clean session persist, then
  terminates the process tree.
- Unix runs are session/process-group leaders; `SIGTERM` is followed by
  `SIGKILL` after `process_grace_seconds` (default 5). Windows runs are assigned
  to a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`.
- The hub enforces a controller lease per run so two devices cannot type into
  the same terminal, and the host enforces one AgentNexus run per native
  session.
- A best-effort process scan warns when the same native session appears to be
  open in another terminal.

## Verified by tests

| Claim | Evidence |
| --- | --- |
| A real PTY run starts, streams output and stops through the host link | `internal/host/daemon_link_test.go` `TestDaemonHubLinkAndTerminalRun` (Windows locally, CI all OS) |
| Policy rejects out-of-root and forbidden-flag commands before spawning | `internal/host/runs_policy_test.go`, `TestDaemonHubLinkAndTerminalRun` |
| Stopping a run kills a grandchild on the same OS | `internal/host/process_tree_test.go` (Windows locally, CI all OS) |
| Resize and write paths | `internal/host/runs.go` is exercised by the link test; no dedicated PTY resize assertion |
| Concurrency guard across modes | `internal/host/runs_policy_test.go` `TestSessionConflictAcrossModes` |

## Not verified

- Rendering, colors, IME input and bracketed paste in xterm.js against every
  real agent.
- Ctrl-C behavior when an agent traps or ignores SIGINT.
- Unix process-group behavior with real agents on Linux and macOS; CI runs the
  grandchild test but not a real agent CLI.
- WSL, tmux/screen wrappers and terminal multiplexers.
- Windows creates the child suspended and resumes it only after job
  assignment, so the old post-create race is closed. Remaining risks are the
  undocumented `NtResumeProcess` call and a failed job assignment; see
  SECURITY.md.

## Fallbacks

- If a PTY cannot be created, the run fails with a clear error and the session
  stays untouched.
- If structured mode fails, terminal mode is the fallback.
- Process-tree termination is best effort on exotic wrappers; the native store
  remains the source of truth even if a stray process survives.
