# @multica/mcp

A [Model Context Protocol](https://modelcontextprotocol.io) server for Multica.
Exposes Multica resources — issues, agents, channels, projects, autopilots — as
MCP tools so any MCP-aware AI assistant (Claude Desktop, Claude Code, Cursor,
etc.) can orchestrate Multica directly from chat.

The server is **self-contained**: it depends on the published MCP SDK and `zod`,
nothing else from the Multica monorepo. It talks to Multica over the same HTTP
API the web/desktop apps use; the local `multica` CLI does **not** need to be
installed for the server to run.

## What's exposed

| Group | Tools |
| --- | --- |
| Issues | `multica_issue_list`, `multica_issue_search`, `multica_issue_get`, `multica_issue_create`, `multica_issue_update`, `multica_issue_status`, `multica_issue_assign`, `multica_issue_comment_add`, `multica_issue_comment_list`, `multica_issue_runs` |
| Agents | `multica_agent_list`, `multica_agent_get`, `multica_agent_tasks` |
| Channels | `multica_channel_list`, `multica_channel_get`, `multica_channel_history`, `multica_channel_post`, `multica_channel_members`, `multica_channel_mark_read` |
| Projects | `multica_project_list`, `multica_project_get`, `multica_project_search`, `multica_project_create` |
| Labels | `multica_label_list`, `multica_label_attach`, `multica_label_detach` |
| Autopilots | `multica_autopilot_list`, `multica_autopilot_get`, `multica_autopilot_runs`, `multica_autopilot_trigger` |
| Workspace | `multica_workspace_get`, `multica_workspace_members` |

Read tools are exposed broadly. Mutating tools err on the side of caution: agent
configuration, autopilot creation, label CRUD, and workspace-membership changes
are deliberately **not** exposed because they have wider blast radius and are
better-driven from the form-based UIs.

## Requirements

- Node.js 20 or newer.
- A Multica personal access token (`mul_…`) and the workspace UUID you want the
  server to operate against.

## Installation

From the monorepo:

```bash
pnpm install
pnpm --filter @multica/mcp build
```

The build produces a single bundled file at `packages/mcp/dist/index.js` with a
`#!/usr/bin/env node` shebang and the `multica-mcp` bin entry pointed at it.

## Configuration

Two sources, first-wins:

1. **Environment variables** (recommended for MCP client configs):
   - `MULTICA_API_URL` — HTTP base URL, e.g. `https://multica.example.com` or
     `http://localhost:8080`. **No** trailing slash, **no** `/ws` suffix.
   - `MULTICA_TOKEN` — bearer token (`mul_…`). Generate one with `multica auth
     token create` or in the workspace settings UI.
   - `MULTICA_WORKSPACE_ID` — default workspace UUID. Tools that don't take an
     explicit `workspace_id` argument operate against this one.
2. **`~/.multica/config.json`** (the file the `multica` CLI writes on `multica
   login`). Used as a fallback when the env vars above are unset. The CLI stores
   a WebSocket URL like `wss://api.example/ws`; the MCP server derives the HTTP
   base by swapping the scheme and stripping the `/ws` suffix.

## Wiring it into an MCP client

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS):

```json
{
  "mcpServers": {
    "multica": {
      "command": "node",
      "args": ["/absolute/path/to/multica/packages/mcp/dist/index.js"],
      "env": {
        "MULTICA_API_URL": "https://your-multica.example.com",
        "MULTICA_TOKEN": "mul_…",
        "MULTICA_WORKSPACE_ID": "00000000-0000-0000-0000-000000000000"
      }
    }
  }
}
```

Restart Claude Desktop after editing the file.

### Claude Code

```bash
claude mcp add multica \
  --env MULTICA_API_URL=https://your-multica.example.com \
  --env MULTICA_TOKEN=mul_… \
  --env MULTICA_WORKSPACE_ID=00000000-0000-0000-0000-000000000000 \
  -- node /absolute/path/to/multica/packages/mcp/dist/index.js
```

(Or omit `--env` flags entirely if the running shell has those variables set
and you'd rather inherit them. The new MCP server is available the next time you
start a Claude Code session.)

### Cursor / Windsurf / other MCP clients

Any client that speaks the standard MCP stdio transport works. The command is
`node /absolute/path/to/dist/index.js` plus the three env vars above.

## Output format

Tool results are returned as a single text content block containing pretty-
printed JSON of the underlying Multica API response. The model is expected to
parse it; the server does **not** strip or reshape fields. This keeps the
contract one-to-one with Multica's REST API and lets future server changes flow
through without an MCP-side translation layer.

Errors (HTTP 4xx/5xx, validation failures, network errors) come back as `is
Error: true` content blocks with the upstream error body included so the model
can recover instead of hard-stopping.

## Development

```bash
pnpm --filter @multica/mcp typecheck   # tsc --noEmit
pnpm --filter @multica/mcp test        # vitest
pnpm --filter @multica/mcp dev         # tsup --watch
```

The dev build is a single bundled `dist/index.js` you can `node` against to
smoke-test outside of an MCP client.

## Layout

```
packages/mcp/
├── README.md
├── package.json          # name: @multica/mcp, bin: multica-mcp
├── tsconfig.json         # extends @multica/tsconfig/base.json
├── tsup.config.ts        # bundle to dist/index.js with a node20 shebang
└── src/
    ├── index.ts          # CLI entry — parses env, connects stdio transport
    ├── server.ts         # Builds Server, registers tools, dispatches calls
    ├── client.ts         # Tiny self-contained Multica HTTP client
    ├── config.ts         # Env / ~/.multica/config.json loader
    ├── tool.ts           # ToolDefinition contract + defineTool helper
    └── tools/
        ├── index.ts      # Aggregates all tool modules into `allTools`
        ├── workspace.ts
        ├── issues.ts
        ├── agents.ts
        ├── channels.ts
        ├── projects.ts
        ├── labels.ts
        └── autopilots.ts
```

Adding a new tool is a matter of dropping a new file under `src/tools/`,
exporting an array of `defineTool({ … })` instances, and listing the array in
`tools/index.ts`. No other wiring required — the server picks them up via
`allTools`.
