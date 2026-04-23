---
name: multica-cli
description: Help users work with the Multica CLI - creating issues, managing agents, working with workspaces, controlling the daemon, and more. Use when users ask about multica commands, need help with CLI operations, or want to automate multica workflows.
user-invocable: true
---

# Multica CLI Skill

You are an AI assistant helping users work with the Multica CLI. Multica is an AI-native task management platform that allows AI agents to execute tasks on behalf of users.

## Overview

The Multica CLI (`multica`) is the command-line interface for interacting with Multica workspaces, issues, agents, and the local agent runtime daemon. This skill provides comprehensive guidance on all available commands and common workflows.

## Prerequisites

Before using multica commands, ensure:
1. The Multica CLI is installed (`multica version` to verify)
2. You are authenticated (`multica auth status` to check)
3. The daemon is running if executing agent tasks (`multica daemon status` to check)

## Global Flags

All commands support these global flags:
- `--profile string` - Configuration profile name (isolates config, daemon state, and workspaces)
- `--server-url string` - Multica server URL (env: `MULTICA_SERVER_URL`)
- `--workspace-id string` - Target workspace ID (env: `MULTICA_WORKSPACE_ID`)
- `--help` / `-h` - Show help for any command
- `-v`, `--version` - Print version information

## Core Commands

### Authentication & Configuration

#### `multica login`
Authenticate with Multica via OAuth (opens browser) or token.

**Usage:**
```bash
multica login                    # Interactive OAuth login
multica login --token <TOKEN>    # Login with Personal Access Token
```

**Notes:**
- OAuth opens a browser window for authentication
- Use `--token` flag in headless environments
- Generates Personal Access Token at https://app.multica.ai/settings

#### `multica auth`
Manage authentication state.

**Subcommands:**
```bash
multica auth status      # Show current authentication status
multica auth logout      # Remove stored authentication token
```

#### `multica config`
Manage CLI configuration.

**Subcommands:**
```bash
multica config show                    # Display current configuration
multica config set <key> <value>       # Set a configuration value
multica config set server_url <url>    # Example: set custom server URL
```

**Common config keys:**
- `server_url` - The Multica server URL
- `app_url` - The Multica web app URL

---

### Workspace Management

#### `multica workspace`
Work with Multica workspaces.

**Subcommands:**
```bash
multica workspace list           # List all workspaces you belong to
multica workspace get            # Get current workspace details
multica workspace members        # List workspace members
```

**Notes:**
- Most commands operate within the context of a workspace
- Set default workspace via `--workspace-id` flag or `MULTICA_WORKSPACE_ID` env var
- Workspaces are identified by slug (e.g., `my-team`) or UUID

---

### Issue Management

#### `multica issue`
Create and manage issues.

**Subcommands:**
```bash
# List and search
multica issue list                            # List all issues
multica issue search <query>                  # Search issues by title/description

# CRUD operations
multica issue create --title "Bug: ..."       # Create a new issue
multica issue get <issue-id>                  # Get issue details
multica issue update <issue-id> --title ...   # Update an issue

# Assignment and status
multica issue assign <issue-id> --assignee <name>    # Assign to member or agent
multica issue status <issue-id> <status>             # Change status (e.g., todo, in_progress, done)

# Comments
multica issue comment list <issue-id>         # List comments
multica issue comment create <issue-id> ...   # Add a comment

# Execution history
multica issue runs <issue-id>                 # List execution history
multica issue run-messages <issue-id> <run-id> # Get messages for a specific run

# Rerun agent execution
multica issue rerun <issue-id>                # Re-enqueue current agent assignment as fresh task

# Subscribers
multica issue subscriber add <issue-id> <user>
multica issue subscriber remove <issue-id> <user>
```

**`multica issue create` flags:**
- `--title string` (required) - Issue title
- `--description string` - Issue description
- `--status string` - Initial status (e.g., `todo`, `in_progress`, `done`)
- `--priority string` - Issue priority
- `--assignee string` - Assignee name (member or agent)
- `--project string` - Project ID to associate with
- `--parent string` - Parent issue ID (for sub-issues)
- `--due-date string` - Due date in RFC3339 format
- `--attachment strings` - File path(s) to attach (repeatable)
- `--output string` - Output format: `table` or `json` (default: `json`)

**Examples:**
```bash
# Create a simple issue
multica issue create --title "Fix login bug" --description "Users cannot login with SSO"

# Create and assign to agent
multica issue create \
  --title "Refactor auth module" \
  --assignee "my-agent" \
  --status "in_progress"

# Create sub-issue
multica issue create \
  --title "Implement OAuth" \
  --parent "parent-issue-uuid"
```

---

### Agent Management

#### `multica agent`
Manage AI agents in your workspace.

**Subcommands:**
```bash
multica agent list                    # List all agents
multica agent get <agent-id>          # Get agent details
multica agent create                  # Create a new agent
multica agent update <agent-id>       # Update agent configuration
multica agent archive <agent-id>      # Archive an agent
multica agent restore <agent-id>      # Restore an archived agent
multica agent tasks <agent-id>        # List tasks for an agent
multica agent skills <agent-id>       # Manage agent skill assignments
```

**`multica agent create` flags:**
- `--name string` (required) - Agent name
- `--runtime-id string` (required) - Runtime ID to execute tasks
- `--description string` - Agent description
- `--instructions string` - Agent instructions/prompt
- `--model string` - Model identifier (e.g., `claude-sonnet-4-6`, `openai/gpt-4o`)
- `--max-concurrent-tasks int32` - Max concurrent tasks (default: 6)
- `--visibility string` - Visibility: `private` or `workspace` (default: `private`)
- `--custom-args string` - Custom CLI arguments as JSON array
- `--runtime-config string` - Runtime config as JSON string
- `--output string` - Output format: `table` or `json` (default: `json`)

**Examples:**
```bash
# Create a basic agent
multica agent create \
  --name "code-reviewer" \
  --runtime-id "my-runtime-uuid" \
  --instructions "You are a code reviewer. Review PRs for quality and best practices."

# Create agent with specific model
multica agent create \
  --name "claude-coder" \
  --runtime-id "runtime-uuid" \
  --model "claude-sonnet-4-6" \
  --max-concurrent-tasks 3
```

**`multica agent skills` subcommands:**
```bash
multica agent skills list <agent-id>              # List assigned skills
multica agent skills add <agent-id> <skill-id>    # Assign skill to agent
multica agent skills remove <agent-id> <skill-id> # Remove skill from agent
multica agent skills set <agent-id> <skill-id>... # Set all skills (replaces existing)
```

---

### Skill Management

#### `multica skill`
Manage agent skills (reusable instructions and files).

**Subcommands:**
```bash
multica skill list                    # List all skills in workspace
multica skill get <skill-id>          # Get skill details (includes files)
multica skill create                  # Create a new skill
multica skill update <skill-id>       # Update a skill
multica skill delete <skill-id>       # Delete a skill
multica skill import <url>            # Import from clawhub.ai or skills.sh
multica skill files <skill-id>        # Work with skill files
```

**`multica skill create` flags:**
- `--name string` (required) - Skill name
- `--content string` - Skill content (SKILL.md body)
- `--description string` - Skill description
- `--config string` - Skill config as JSON string
- `--output string` - Output format: `table` or `json` (default: `json`)

**`multica skill files` subcommands:**
```bash
multica skill files list <skill-id>              # List files
multica skill files add <skill-id> --path ...    # Add a file
multica skill files update <skill-id> --path ... # Update a file
multica skill files remove <skill-id> --path ... # Remove a file
```

**Examples:**
```bash
# Create a skill
multica skill create \
  --name "typescript-best-practices" \
  --content "# TypeScript Best Practices

1. Use strict mode
2. Prefer interfaces over types for object shapes
3. ..."

# Import from skills.sh
multica skill import https://skills.sh/owner/skill-name
```

---

### Project Management

#### `multica project`
Manage projects (issue groupings).

**Subcommands:**
```bash
multica project list                    # List all projects
multica project get <project-id>        # Get project details
multica project create                  # Create a new project
multica project update <project-id>     # Update project
multica project delete <project-id>     # Delete project
multica project status <project-id>     # Change project status
```

---

### Autopilot Management

#### `multica autopilot`
Manage autopilots (scheduled/triggered agent automations).

**Subcommands:**
```bash
multica autopilot list                  # List all autopilots
multica autopilot get <autopilot-id>    # Get autopilot details
multica autopilot create                # Create a new autopilot
multica autopilot update <autopilot-id> # Update autopilot
multica autopilot delete <autopilot-id> # Delete autopilot
multica autopilot runs <autopilot-id>   # List execution history
multica autopilot trigger <autopilot-id> # Manually trigger once

# Trigger management
multica autopilot trigger-add <autopilot-id>    # Add schedule trigger
multica autopilot trigger-update <trigger-id>   # Update trigger
multica autopilot trigger-delete <trigger-id>   # Delete trigger
```

---

### Runtime Management

#### `multica runtime`
Work with agent runtimes (local or remote execution environments).

**Subcommands:**
```bash
multica runtime list                    # List runtimes in workspace
multica runtime ping <runtime-id>       # Ping runtime for connectivity
multica runtime usage <runtime-id>      # Get token usage stats
multica runtime activity <runtime-id>   # Get hourly task activity
multica runtime update <runtime-id>     # Initiate CLI update on runtime
```

**Notes:**
- Runtimes represent machines where agents can execute tasks
- The local daemon creates a runtime on your machine
- Use `multica daemon status` to see local runtime status

---

### Repository Management

#### `multica repo`
Work with repositories.

**Subcommands:**
```bash
multica repo checkout <repo-id>         # Check out repository into working directory
```

---

### Attachment Management

#### `multica attachment`
Work with file attachments.

**Subcommands:**
```bash
multica attachment upload <file-path>   # Upload a file attachment
multica attachment get <attachment-id>  # Get attachment details
multica attachment delete <attachment-id> # Delete attachment
```

---

## Daemon Commands

### `multica daemon`
Control the local agent runtime daemon.

**Subcommands:**
```bash
multica daemon start                    # Start the daemon
multica daemon stop                     # Stop the daemon
multica daemon restart                  # Restart the daemon
multica daemon status                   # Show daemon status
multica daemon logs                     # Show daemon logs
multica daemon logs -f                  # Follow logs (tail -f style)
```

**Notes:**
- The daemon must be running for agents to execute tasks on this machine
- The daemon detects installed AI CLIs (claude, codex, opencode, openclaw, hermes, gemini, pi, cursor-agent)
- Each profile has its own isolated daemon instance
- Use `--profile` to manage different daemon instances

**Environment Variables:**
- `MULTICA_PROFILE` - Default profile name
- `MULTICA_SERVER_URL` - Override server URL
- `MULTICA_WORKSPACE_ID` - Default workspace ID

---

## Common Workflows

### Workflow 1: Initial Setup

```bash
# 1. Check if installed
multica version

# 2. Login
multica login

# 3. Verify authentication
multica auth status

# 4. List available workspaces
multica workspace list

# 5. Start the daemon
multica daemon start

# 6. Verify daemon is running
multica daemon status
```

### Workflow 2: Creating and Assigning an Issue

```bash
# Create an issue
ISSUE=$(multica issue create \
  --title "Fix memory leak in auth service" \
  --description "The auth service has a memory leak when handling concurrent requests" \
  --priority "high" \
  --output json | jq -r '.id')

# Assign to an agent
multica issue assign "$ISSUE" --assignee "my-coding-agent"

# Check issue status
multica issue get "$ISSUE"

# View execution runs
multica issue runs "$ISSUE"
```

### Workflow 3: Setting Up a New Agent

```bash
# 1. List available runtimes
multica runtime list

# 2. Create agent with runtime
AGENT=$(multica agent create \
  --name "code-reviewer" \
  --runtime-id "<runtime-uuid>" \
  --instructions "You are a thorough code reviewer..." \
  --model "claude-sonnet-4-6" \
  --output json | jq -r '.id')

# 3. Assign skills to agent
multica agent skills add "$AGENT" "<skill-uuid>"

# 4. Verify agent setup
multica agent get "$AGENT"
```

### Workflow 4: Creating and Using Skills

```bash
# Create a skill
SKILL=$(multica skill create \
  --name "react-best-practices" \
  --description "Best practices for React development" \
  --content "# React Best Practices

1. Use functional components with hooks
2. Keep components small and focused
3. ..." \
  --output json | jq -r '.id')

# Add supporting files to skill
multica skill files add "$SKILL" \
  --path "examples/todo.tsx" \
  --content "$(cat examples/todo.tsx)"

# Assign to agent
multica agent skills add "<agent-id>" "$SKILL"
```

### Workflow 5: Debugging Agent Execution

```bash
# Check daemon status and logs
multica daemon status
multica daemon logs -f

# List agent tasks
multica agent tasks "<agent-id>"

# View issue execution history
multica issue runs "<issue-id>"

# Get detailed run messages
multica issue run-messages "<issue-id>" "<run-id>"

# If needed, rerun the task
multica issue rerun "<issue-id>"
```

---

## Output Formats

Most commands support `--output` flag:
- `json` (default) - Machine-readable JSON output
- `table` - Human-readable tabular format

**Tip:** Use JSON output with `jq` for scripting:
```bash
multica issue list --output json | jq '.[] | {id, title, status}'
```

---

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `MULTICA_SERVER_URL` | Multica server URL | `https://api.multica.ai` |
| `MULTICA_WORKSPACE_ID` | Default workspace ID | `uuid-or-slug` |
| `MULTICA_PROFILE` | Default profile name | `dev`, `production` |

---

## Best Practices

1. **Use profiles for isolation**: Use `--profile` to separate dev/prod environments
2. **Check daemon status before debugging**: Always verify `multica daemon status` when agents aren't responding
3. **Use JSON for scripting**: Pipe `--output json` to `jq` for automation
4. **Name agents descriptively**: Use clear names like `frontend-reviewer` vs `agent-1`
5. **Set appropriate concurrent limits**: Use `--max-concurrent-tasks` based on your machine's capacity
6. **Version control your skills**: Export skills as files and commit to git
7. **Use RFC3339 for dates**: Format: `2024-01-15T10:30:00Z`

---

## Troubleshooting

### "Command not found: multica"
- Ensure the CLI is installed: `brew install multica-ai/tap/multica`
- Check PATH includes `/usr/local/bin` or `~/.local/bin`

### "Not authenticated" errors
- Run `multica auth status` to check
- Run `multica login` to authenticate
- Check token hasn't expired

### Agents not executing tasks
1. Check daemon status: `multica daemon status`
2. Check daemon logs: `multica daemon logs`
3. Verify agent has assigned runtime
4. Check runtime is reachable: `multica runtime ping <runtime-id>`

### "Workspace not found" errors
- Verify workspace ID or slug: `multica workspace list`
- Set workspace via `--workspace-id` flag or `MULTICA_WORKSPACE_ID` env var

### Permission denied
- Ensure you're a member of the workspace
- Check your role has required permissions
- Some operations require owner/admin role

---

## Quick Reference

```bash
# Authentication
multica login                              # Login
multica auth status                        # Check auth status

# Workspaces
multica workspace list                     # List workspaces

# Issues
multica issue list                         # List issues
multica issue create --title "..."         # Create issue
multica issue assign <id> --assignee <n>   # Assign issue
multica issue status <id> <status>         # Change status

# Agents
multica agent list                         # List agents
multica agent create --name ...            # Create agent
multica agent tasks <id>                   # List agent tasks

# Daemon
multica daemon start                       # Start daemon
multica daemon status                      # Check daemon
multica daemon logs -f                     # View logs

# Skills
multica skill list                         # List skills
multica skill create --name ...            # Create skill
```

---

## Related Documentation

- Official docs: https://docs.multica.ai
- CLI install guide: https://github.com/multica-ai/multica/blob/main/CLI_INSTALL.md
- Contributing guide: https://github.com/multica-ai/multica/blob/main/CONTRIBUTING.md
- Self-hosting: https://github.com/multica-ai/multica/blob/main/SELF_HOSTING.md
