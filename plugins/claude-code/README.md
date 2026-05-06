# Multica Plugin for Claude Code

Manage Multica issues, projects, and agents without leaving the terminal.

## Install

```bash
# From marketplace
claude plugin install multica

# Or local
claude --plugin-dir ./plugins/claude-code
```

## Prerequisites

```bash
# Install Multica CLI
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash

# Login
multica login
```

## What You Can Do

Just talk naturally:

- "show me my in-progress issues"
- "create an issue for the login bug, high priority"
- "assign PROJ-42 to codex"
- "mark PROJ-42 as done"
- "what's the status of the Auth project?"
- "add a comment to PROJ-42: fixed in commit abc123"

## How It Works

This plugin teaches Claude Code how to use the `multica` CLI. No extra servers, no APIs — just your existing CLI.
