# @multica/mcp

MCP (Model Context Protocol) server for Multica — file and manage work items from any AI agent.

## What it does

Exposes your Multica instance as MCP tools that any AI agent can use:

| Tool | Description |
|------|-------------|
| `create_issue` | Create a work item with title, description, priority, and optional assignee |
| `list_issues` | List issues with optional filters (status, priority) |
| `get_issue` | Get full issue details including comments |
| `update_issue` | Update status, priority, title, or description |
| `assign_issue` | Assign to an agent (Copilot, Codex) or member by name |
| `add_comment` | Add a comment to an issue |
| `list_agents` | List available agents and their status |

## Setup

### 1. Get a Personal Access Token

In the Multica web UI: **Settings → Tokens → Create Token**

### 2. Add to your MCP config

**Claude Code** (`~/.claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "multica": {
      "command": "npx",
      "args": ["tsx", "/path/to/multica/packages/mcp/src/index.ts"],
      "env": {
        "MULTICA_URL": "http://localhost:8080",
        "MULTICA_TOKEN": "mul_your_token_here"
      }
    }
  }
}
```

**GitHub Copilot CLI** (`~/.copilot/mcp-config.json`):

```json
{
  "mcpServers": {
    "multica": {
      "command": "npx",
      "args": ["tsx", "/path/to/multica/packages/mcp/src/index.ts"],
      "env": {
        "MULTICA_URL": "http://localhost:8080",
        "MULTICA_TOKEN": "mul_your_token_here"
      }
    }
  }
}
```

### 3. Use it

In any agent conversation:

> "File a work item: Add dark mode to the settings page. High priority. Assign to Copilot."

The agent calls `create_issue` → `assign_issue` via MCP. Your Multica daemon picks it up, and the assigned agent starts working.

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MULTICA_URL` | Yes | Your Multica server URL (e.g. `http://localhost:8080`) |
| `MULTICA_TOKEN` | Yes | Personal Access Token (`mul_...`) |

## Development

```bash
# Run directly with tsx
MULTICA_URL=http://localhost:8080 MULTICA_TOKEN=mul_xxx npx tsx src/index.ts

# Build
pnpm build

# Run built version
MULTICA_URL=http://localhost:8080 MULTICA_TOKEN=mul_xxx node dist/index.js
```

## With Tailscale

If your Multica runs on a Mac Mini accessible via Tailscale:

```json
{
  "env": {
    "MULTICA_URL": "http://multica:8080",
    "MULTICA_TOKEN": "mul_your_token_here"
  }
}
```
