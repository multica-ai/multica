# Remote MCP server for Multica

A small, self-hosted [MCP](https://modelcontextprotocol.io) server that lets chat clients — claude.ai on web and
mobile, Claude Desktop, Claude Code, or any client that speaks Streamable HTTP — work with Multica issues without a
local terminal. Each person connects with their own key and acts as themselves in Multica.

It exposes a single tool, `multica`, that runs the existing `multica` CLI. The server adds no task logic of its own:
it checks the command against an explicit allowlist, picks the caller's personal access token (PAT) and workspace,
runs the CLI without a shell and writes an audit line. Whatever the CLI can do on the allowlist, the tool can do, and
its `--help` is the tool's documentation.

Python 3.10+, standard library only.

**Scope.** Issue work over remote MCP: read, search, create, update, assign, change status, comment, subscribe,
label, plus read-only lookups (projects, agents, squads, labels, workspaces). Not included: a stdio transport,
OAuth sign-in, agent/skill/autopilot management, and deletion of anything.

## The `multica` tool

| Argument | Type | Meaning |
|---|---|---|
| `args` | array of strings, required | argv after the binary name, e.g. `["issue", "get", "MUL-123"]`. No shell: `;`, `$(`, `\|` are plain text. |
| `stdin` | string, up to 256 KB | Text for `--content-stdin` / `--description-stdin`. |
| `workspace` | string | Workspace slug, name or id. Required only when the key allows more than one. |

The result is `{"exit_code", "stdout", "stderr", "truncated", "timeout"}`; a non-zero exit or a timeout sets
`isError: true`. `--output json` is appended unless the call already has `--output`, asks for `--help`, or is `version`.

```json
{"args": ["issue", "list", "--status", "todo", "--limit", "20"]}
{"args": ["issue", "comment", "add", "MUL-123", "--content-stdin"], "stdin": "Multi-line\ncomment"}
{"args": ["issue", "create", "--title", "Fix login", "--assignee", "Lambda", "--description-stdin"], "stdin": "## Why\n..."}
```

`issue create`/`update` with `--assignee` and `issue assign` start an agent run; `issue comment add` wakes the assignee.

### Allowed commands

The single source of truth is `COMMANDS` in [`multica_mcp/policy.py`](multica_mcp/policy.py). Anything not listed
— including CLI subcommands added later — is rejected with JSON-RPC error `-32602` before a process starts, and the
denial is audited.

| Access | Commands |
|---|---|
| read | `issue list`, `get`, `search`, `children`, `pull-requests`, `timeline`, `runs`, `run-messages`, `usage`; `issue comment list`, `issue subscriber list`, `issue label list`, `issue metadata list\|get`; `project list\|get`, `agent list\|get`, `squad list\|get`, `label list`, `workspace list`, `version` |
| write | `issue create`, `update`, `assign`, `status`; `issue comment add\|update\|resolve\|unresolve`; `issue subscriber add\|remove`; `issue label add\|remove` |
| help | `--help` / `-h` on any allowed command or group |

Rejected everywhere: `*-file` flags and `--allow-external-file`, `--attachment*` (they read files on the server
host), and the global `--server-url`, `--profile`, `--workspace-id`, `--debug` (they would send the token elsewhere or
bypass the workspace allowlist). A key marked read-only gets only the read rows.

## Run it

You need a Multica PAT (**Settings → API Token**, prefix `mul_`) and the `multica` CLI matching your server version
([install](../../CLI_INSTALL.md)).

```sh
cd examples/remote-mcp
export MULTICA_API_URL=https://api.multica.ai        # or your self-hosted API
export MULTICA_PAT=mul_...                           # the person this key acts as
export MULTICA_MCP_KEY=$(openssl rand -hex 32)       # what the MCP client sends; keep it secret
export MULTICA_MCP_WORKSPACES=my-team                # workspace slugs or ids this key may use
export MULTICA_MCP_STATE_DIR=$HOME/.local/state/multica-mcp
python3 -m multica_mcp                               # listens on 127.0.0.1:8101
```

Check it:

```sh
curl -s localhost:8101/mcp -H "Authorization: Bearer $MULTICA_MCP_KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"multica","arguments":{"args":["issue","list","--limit","3"]}}}'
```

### Docker

[`Dockerfile`](Dockerfile) copies the CLI from the backend image; set `MULTICA_BACKEND_IMAGE` to the tag your server
runs. [`compose.yaml`](compose.yaml) is a hardened example (read-only root, no capabilities, tmpfs for run
directories, audit log on a volume) meant to sit behind a reverse proxy on the same host:

```sh
cat > .env <<EOF
MULTICA_BACKEND_IMAGE=ghcr.io/multica-ai/multica-backend:v0.7.1
MULTICA_API_URL=https://api.multica.ai
MULTICA_PAT=mul_...
MULTICA_MCP_KEY=$(openssl rand -hex 32)
MULTICA_MCP_WORKSPACES=my-team
MULTICA_MCP_ALLOWED_HOSTS=mcp.example.com
EOF
chmod 600 .env
docker compose up -d --build
```

### Public endpoint

Chat clients such as claude.ai call the server from the provider's cloud, so it needs an HTTPS URL. Terminate TLS in
a reverse proxy on the same host and forward only `/mcp`; add the public host name to `MULTICA_MCP_ALLOWED_HOSTS`.
Caddy example:

```caddyfile
mcp.example.com {
	handle /mcp {
		request_body {
			max_size 1MB
		}
		reverse_proxy 127.0.0.1:8101 {
			header_up X-Forwarded-For {remote_host}
		}
	}
	respond 404
}
```

The server trusts `X-Forwarded-For` only from loopback peers and uses it for the audit log and the per-address limit
on failed key attempts.

## Connect a client

**Claude Code**

```sh
claude mcp add --transport http multica https://mcp.example.com/mcp --header "Authorization: Bearer <key>"
```

**claude.ai / Claude Desktop**: *Customize → Connectors → Add custom connector*, URL `https://mcp.example.com/mcp`,
no sign-in, request header `Authorization: Bearer <key>`. Menu names change from time to time; see Anthropic's
[custom connector guide](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp).

Other clients: any Streamable HTTP MCP client that can send a bearer header.

## Configuration

All settings come from the environment; an invalid setting stops the server with exit code 2 before it listens.

| Variable | Default | Meaning |
|---|---|---|
| `MULTICA_API_URL` | `https://api.multica.ai` | Multica API the CLI talks to (`http(s)://host[:port]`, no path) |
| `MULTICA_MCP_KEY` + `MULTICA_PAT` | — | The default client: its bearer key (32+ chars) and the PAT it acts as |
| `MULTICA_MCP_WORKSPACES` | — | Comma-separated slugs or ids for the default client, and the default for the clients file |
| `MULTICA_MCP_READ_ONLY` | `false` | Restrict the default client to read commands |
| `MULTICA_MCP_CLIENTS` | — | Path to the per-person clients file (below) |
| `MULTICA_MCP_STATE_DIR` | — | Where `audit.jsonl` goes; `STATE_DIRECTORY` from systemd wins |
| `MULTICA_BIN` | `multica` | CLI path |
| `MULTICA_MCP_HOST` / `MULTICA_MCP_PORT` | `127.0.0.1` / `8101` | Listen address |
| `MULTICA_MCP_ALLOWED_HOSTS` | `127.0.0.1,localhost` | Accepted `Host` header names (DNS-rebinding guard) |
| `MULTICA_MCP_ALLOWED_ORIGINS` | `https://claude.ai,https://claude.com` | Accepted `Origin` values; requests without `Origin` are accepted |
| `MULTICA_MCP_RUN_DIR` | system temp | Parent of the empty per-call working directories |

At least one client is required: the default one, the clients file, or both.

### Per-person keys

To give several people access, each with their own PAT, point `MULTICA_MCP_CLIENTS` at a JSON file that only the
service user can read (`chmod 600`; the server refuses a group- or world-readable file):

```json
[
  {"name": "alice", "key": "<openssl rand -hex 32>", "token": "mul_...", "workspaces": ["my-team"]},
  {"name": "bob", "key": "<openssl rand -hex 32>", "token": "mul_...", "read_only": true}
]
```

Names and keys must be unique. The file is read at startup; restart after editing. To revoke a person, remove their
row, restart, and revoke their PAT in Multica.

## Security model

- **Permissions come from the PAT.** The server never widens what the person can do in Multica; it only narrows it
  with the command allowlist, the read-only flag and the workspace list. The backend scopes every issue lookup to the
  selected workspace and checks membership.
- **Workspace.** The CLI gets `MULTICA_WORKSPACE_ID` from the key's allowed list (resolved once per client with
  `workspace list` and the client's own token); `--workspace-id` is rejected.
- **Process.** `subprocess` with an argv list, no shell; an empty temporary `cwd` that is also `HOME`; the environment
  is only `MULTICA_TOKEN`, `MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID`, `HOME` and `PATH`. 30 s timeout, then the
  process group is killed. At most 4 runs at a time. The token never appears in argv.
- **Output.** stdout and stderr are capped at 64 KB each. Every configured key and PAT is masked in output, error
  messages and the audit log, before truncation, so a secret on the boundary does not leak in halves.
- **Transport.** `POST /mcp` with `Authorization: Bearer <key>`, compared in constant time against every key.
  Rejections in order: path (404), `Host` (403), `Origin` (403), 10 failed keys per minute per address (429), key
  (401), body over 1 MB (413). Stateless: no sessions or SSE, `GET /mcp` is 405. An unsupported
  `MCP-Protocol-Version` is 400 before any tool runs. `GET /healthz` answers on loopback only.
- **Audit.** `audit.jsonl` (0600), one line per call: `client`, `client_ip`, `argv`, `exit_code`, `timeout`,
  `duration`, sizes, and `denied` with the reason. For `stdin`: sha256, length and the first 200 masked characters.

## Tests

```sh
cd examples/remote-mcp
python3 -W error::ResourceWarning -m unittest discover -s tests
```

Unit tests replace the CLI with a fake script that records argv, environment and stdin, and drive the real HTTP
handler over a socket pair, so they need no network or server.

`tests/test_integration.py` is opt-in and talks to a real backend with read commands only. It starts the server with
the real CLI, checks allowed reads, denials before the CLI, a read-only key, a workspace the PAT does not belong to,
and the official MCP Python SDK client (`pip install mcp`):

```sh
MULTICA_MCP_IT_BIN=$(command -v multica) MULTICA_MCP_IT_API_URL=https://api.multica.ai \
MULTICA_MCP_IT_PAT=mul_... MULTICA_MCP_IT_WORKSPACE=my-team \
MULTICA_MCP_IT_OTHER_WORKSPACE=other-team MULTICA_MCP_IT_OTHER_ISSUE=<issue uuid in other-team> \
python3 -m unittest discover -s tests -p test_integration.py -v
```
