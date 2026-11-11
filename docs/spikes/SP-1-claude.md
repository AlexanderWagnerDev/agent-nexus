# SP-1: Claude Code sessions

Status: partially verified. Discovery and transcript parsing are fixture-tested;
the real Claude CLI and the ACP adapter were not installed in the development
environment.

## What exists

- Discovery reads `$CLAUDE_CONFIG_DIR` (default `~/.claude`) and walks
  `projects/**/*.jsonl`.
- Each JSONL line is decoded tolerantly. `type`, `sessionId`, `cwd`,
  `gitBranch`, `timestamp`, `summary` and `message.content` are used.
- Message content may be a plain string or a block array. Text blocks become
  transcript text; `tool_use` blocks become tool calls; tool results are kept
  as messages.
- The first usable user message becomes the title; `<system>`-style titles are
  replaced.
- Native resume is `claude --resume <id>`; fork is
  `claude --resume <id> --fork-session`; new sessions start the CLI without a
  resume flag.
- Structured mode is generic ACP. The daemon auto-detects `claude-agent-acp`
  or `claude-code-acp` on `PATH`, or uses `acp_commands.claude`.

## Verified by tests

| Claim | Evidence |
| --- | --- |
| JSONL discovery, metadata, transcript and command building | `internal/host/adapter_test.go` `TestClaudeAdapter` |
| Full-text search over the JSONL store | `internal/host/adapters_extra_test.go` `TestClaudeAndCodexSearch` |
| Malformed JSON does not panic | `internal/host/parsers_fuzz_test.go` `FuzzClaudeContent` |
| ACP start/prompt/stop against a protocol peer | `internal/host/structured_start_test.go` `TestStartACPStructuredAgainstFakeAgent` (fake ACP agent, not Claude) |
| ACP session updates and permissions | `internal/host/structured_acp_test.go` |

## Not verified

- A real Claude Code installation: store layout across recent versions, the
  exact `agents --json` fields, or ACP `session/list` and `session/load`.
- The real `claude-agent-acp` adapter: permissions, model selection, attachment
  content blocks and distribution/licensing behavior. The host implements
  capability-driven model/reasoning/mode selectors and image attachments, but
  this is verified only with an in-process fake ACP agent.
- Claude on Linux and macOS; only the format parsers and fixtures are shared.
- Older transcript shapes that use a different block layout.

## Fallbacks

- Terminal mode always works when the CLI is installed and the ACP adapter is
  missing or fails.
- The transcript parser skips unknown lines instead of failing the session.
- The hub never rewrites Claude's store; AgentNexus resumes through the native
  CLI or ACP session.

## Tested version band

`claude >= 2.0.0 < 3.0.0`. Outside this band structured mode is disabled with a
warning; terminal mode remains available. The band is a test policy, not a
vendor guarantee.
