# Read-only MCP endpoint

AgentNexus exposes a small, read-only Model Context Protocol endpoint for
local agents and MCP clients.

## Create a token

Open **Settings -> Read-only API tokens -> Create token**. The raw token is
shown once; only its SHA-256 hash is stored. Revoking it invalidates every
client immediately. Tokens have the fixed `read` scope and are rate-limited to
120 requests per minute.

## Configure a client

Use a streamable HTTP MCP server entry pointing at `https://<hub>/mcp` with
`Authorization: Bearer <token>`. The endpoint is stateless: `initialize`,
`tools/list` and `tools/call` work over a single POST. `notifications/initialized`
is accepted with HTTP 202.

## Tools

| Tool | Contents |
| --- | --- |
| `list_hosts` | Hosts visible to the token owner, online state and agent inventory |
| `list_sessions` | Session metadata with optional host, agent, query and paging |
| `search_transcripts` | Federated search over online hosts the user may read |
| `get_transcript_page` | A bounded transcript page, read live from the host |
| `get_git_status` | Read-only branch/status/stat/diff for a session |

Every call is audited with the token id and tool name. The endpoint cannot
start, stop, prompt, approve, write Git or modify any hub state. It does not
bypass per-host grants: the token acts as its owner. Host policies still run on
the host.

## Verification

The API test covers authentication, revocation, initialization, the tool list,
successful metadata calls and a tool error. It uses the in-process hub and fake
hosts; a real Claude Desktop/Cursor MCP client was not available in the test
environment.
