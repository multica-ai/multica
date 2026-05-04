# Multica MCP — Installation Guide

> **Audience:** Users who want to use Multica from Claude Code via the Model Context Protocol (MCP). Run a single command and you're done.

## One-Line Install

In your terminal (or Claude Code's bash):

```bash
gh api "repos/firtal-group/firtal-cerebro/contents/scripts/install-mcp.sh?ref=main" --jq '.content' | base64 -d | bash
```

> **Note:** This requires the [GitHub CLI](https://cli.github.com/) (`gh`) with access to the firtal-group organization. Run `gh auth login` first if you haven't authenticated.

The script:
1. Verifies the `claude` CLI is installed (Claude Code).
2. Installs the `multica` CLI if it isn't already.
3. Runs `multica login` if you aren't authenticated.
4. Registers `multica mcp serve` with Claude Code under user scope.

After it finishes, restart Claude Code (if it's running) and run `/mcp` to verify the `multica` server is connected.

---

## Manual Steps

If you prefer to run the steps yourself, or the one-liner fails:

### 1. Install the Claude Code CLI

If `claude --version` does not work, install Claude Code: https://claude.com/claude-code

### 2. Install the Multica CLI

```bash
gh api "repos/firtal-group/firtal-cerebro/contents/scripts/install.sh?ref=main" --jq '.content' | base64 -d | bash
```

Verify:

```bash
multica version
```

### 3. Log in

```bash
multica login
```

A browser window opens for OAuth. Complete sign-in, then return to the terminal.

### 4. Register the MCP server

```bash
multica mcp install
```

This runs `claude mcp add multica --scope user -- $(which multica) mcp serve` under the hood.

Flags:

- `--scope user` (default) — available across all projects on this machine
- `--scope project` — only the current project (writes to `.mcp.json`)
- `--scope local` — only this project, this machine
- `--name <name>` — register under a different name (default: `multica`)

---

## Troubleshooting

**`'claude' CLI not found on PATH`**
Install Claude Code: https://claude.com/claude-code. Make sure `claude --version` works in the same shell where you run the installer.

**`'multica' not found on PATH` after CLI install**
Restart your shell so the updated `PATH` takes effect, then re-run the installer.

**`not authenticated. Run 'multica login' first`** when MCP starts
The MCP server needs an authenticated session. Run `multica login`, then restart Claude Code.

**MCP shows up in `/mcp` but tools fail**
Check that the daemon is also running if your workspace expects it: `multica daemon status`. See `CLI_INSTALL.md` for daemon setup.

---

## Uninstall

```bash
claude mcp remove multica --scope user
```
