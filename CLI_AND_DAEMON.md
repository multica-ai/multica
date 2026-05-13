# CLI and Agent Daemon Guide

The `multica` CLI connects your local machine to Multica. It handles authentication, workspace management, issue tracking, and runs the agent daemon that executes AI tasks locally.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install multica-ai/tap/multica
```

### Build from Source

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
make build
cp server/bin/multica /usr/local/bin/multica
```

### Update

```bash
brew upgrade multica-ai/tap/multica
```

For install script or manual installs, use:

```bash
multica update
```

`multica update` auto-detects your installation method and upgrades accordingly.

## Quick Start

```bash
# One-command setup: configure, authenticate, and start the daemon
multica setup

# For self-hosted (local) deployments:
multica setup self-host
```

Or step by step:

```bash
# 1. Authenticate (opens browser for login)
multica login

# 2. Start the agent daemon
multica daemon start

# 3. Done — agents in your watched workspaces can now execute tasks on your machine
```

`multica login` automatically discovers all workspaces you belong to and adds them to the daemon watch list.

## Authentication

### Browser Login

```bash
multica login
```

Opens your browser for OAuth authentication, creates a 90-day personal access token, and auto-configures your workspaces.

### Token Login

```bash
multica login --token <mul_...>
```

Authenticate using a personal access token directly. Useful for headless environments. Pass `--token=` with an empty value to be prompted interactively (so the token never lands in shell history).

### Check Status

```bash
multica auth status
```

Shows your current server, user, and token validity.

### Logout

```bash
multica auth logout
```

Removes the stored authentication token.

## Agent Daemon

The daemon is the local agent runtime. It detects available AI CLIs on your machine, registers them with the Multica server, and executes tasks when agents are assigned work.

### Start

```bash
multica daemon start
```

By default, the daemon runs in the background and logs to `~/.multica/daemon.log`.

To run in the foreground (useful for debugging):

```bash
multica daemon start --foreground
```

### Stop

```bash
multica daemon stop
```

### Status

```bash
multica daemon status
multica daemon status --output json
```

Shows PID, uptime, detected agents, and watched workspaces.

### Logs

```bash
multica daemon logs              # Last 50 lines
multica daemon logs -f           # Follow (tail -f)
multica daemon logs -n 100       # Last 100 lines
```

### Supported Agents

The daemon auto-detects these AI CLIs on your PATH:

| CLI | Command | Description |
|-----|---------|-------------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `claude` | Anthropic's coding agent |
| [Codex](https://github.com/openai/codex) | `codex` | OpenAI's coding agent |
| [GitHub Copilot CLI](https://docs.github.com/en/copilot) | `copilot` | GitHub's coding agent (model routed by your GitHub entitlement) |
| OpenCode | `opencode` | Open-source coding agent |
| OpenClaw | `openclaw` | Open-source coding agent |
| Hermes | `hermes` | Nous Research coding agent |
| Gemini | `gemini` | Google's coding agent |
| [Pi](https://pi.dev/) | `pi` | Pi coding agent |
| [Cursor Agent](https://cursor.com/) | `cursor-agent` | Cursor's headless coding agent |
| Kimi | `kimi` | Moonshot coding agent |
| Kiro CLI | `kiro-cli` | Kiro ACP coding agent |

You need at least one installed. The daemon registers each detected CLI as an available runtime.

### How It Works

1. On start, the daemon detects installed agent CLIs and registers a runtime for each agent in each watched workspace
2. It polls the server at a configurable interval (default: 3s) for claimed tasks
3. When a task arrives, it creates an isolated workspace directory, spawns the agent CLI, and streams results back
4. Heartbeats are sent periodically (default: 15s) so the server knows the daemon is alive
5. On shutdown, all runtimes are deregistered

### Configuration

Daemon behavior is configured via flags or environment variables:

| Setting | Flag | Env Variable | Default |
|---------|------|--------------|---------|
| Poll interval | `--poll-interval` | `MULTICA_DAEMON_POLL_INTERVAL` | `3s` |
| Heartbeat interval | `--heartbeat-interval` | `MULTICA_DAEMON_HEARTBEAT_INTERVAL` | `15s` |
| Agent timeout | `--agent-timeout` | `MULTICA_AGENT_TIMEOUT` | `2h` |
| Codex semantic inactivity timeout | `--codex-semantic-inactivity-timeout` | `MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT` | `10m` |
| Max concurrent tasks | `--max-concurrent-tasks` | `MULTICA_DAEMON_MAX_CONCURRENT_TASKS` | `20` |
| Daemon ID | `--daemon-id` | `MULTICA_DAEMON_ID` | hostname |
| Device name | `--device-name` | `MULTICA_DAEMON_DEVICE_NAME` | hostname |
| Runtime name | `--runtime-name` | `MULTICA_AGENT_RUNTIME_NAME` | `Local Agent` |
| Workspaces root | — | `MULTICA_WORKSPACES_ROOT` | `~/multica_workspaces` |
| GC enabled | — | `MULTICA_GC_ENABLED` | `true` (set `false`/`0` to disable) |
| GC scan interval | — | `MULTICA_GC_INTERVAL` | `1h` |
| GC TTL (done/cancelled issues) | — | `MULTICA_GC_TTL` | `24h` |
| GC orphan TTL (no `.gc_meta.json`) | — | `MULTICA_GC_ORPHAN_TTL` | `72h` |
| GC artifact TTL (open issues) | — | `MULTICA_GC_ARTIFACT_TTL` | `12h` (set `0` to disable) |
| GC artifact patterns | — | `MULTICA_GC_ARTIFACT_PATTERNS` | `node_modules,.next,.turbo` |

#### Workspace garbage collection

The daemon periodically scans `MULTICA_WORKSPACES_ROOT` and reclaims disk space in three modes:

- **Full task cleanup** — when an issue's status is `done` or `cancelled` and has been idle for `MULTICA_GC_TTL`, the entire task directory is removed.
- **Orphan cleanup** — task directories with no `.gc_meta.json` (e.g. left over from a daemon crash) are removed once they exceed `MULTICA_GC_ORPHAN_TTL`.
- **Artifact-only cleanup** — when a task has been completed for at least `MULTICA_GC_ARTIFACT_TTL` but the issue is still open, regenerable build outputs whose directory basename matches `MULTICA_GC_ARTIFACT_PATTERNS` are removed; the rest of the workdir (source, `.git`, `output/`, `logs/`, `.gc_meta.json`) is preserved so the agent can resume the same workdir on the next task.

Patterns are basename-only — entries containing `/` or `\` are silently dropped — and `.git` subtrees are never descended into. The default list (`node_modules`, `.next`, `.turbo`) is intentionally narrow; extend it per deployment if your repos consistently produce other regenerable directories (for example, `MULTICA_GC_ARTIFACT_PATTERNS=node_modules,.next,.turbo,target,__pycache__`). To disable artifact cleanup entirely, set `MULTICA_GC_ARTIFACT_TTL=0`.

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `MULTICA_CLAUDE_PATH` | Custom path to the `claude` binary |
| `MULTICA_CLAUDE_MODEL` | Override the Claude model used |
| `MULTICA_CLAUDE_ARGS` | Default extra arguments for Claude Code runs |
| `MULTICA_CODEX_PATH` | Custom path to the `codex` binary |
| `MULTICA_CODEX_MODEL` | Override the Codex model used |
| `MULTICA_CODEX_ARGS` | Default extra arguments for Codex runs |
| `MULTICA_COPILOT_PATH` | Custom path to the `copilot` binary |
| `MULTICA_COPILOT_MODEL` | Override the Copilot model used (note: GitHub Copilot routes models through your account entitlement, so this may not be honoured) |
| `MULTICA_OPENCODE_PATH` | Custom path to the `opencode` binary |
| `MULTICA_OPENCODE_MODEL` | Override the OpenCode model used |
| `MULTICA_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `MULTICA_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `MULTICA_HERMES_PATH` | Custom path to the `hermes` binary |
| `MULTICA_HERMES_MODEL` | Override the Hermes model used |
| `MULTICA_GEMINI_PATH` | Custom path to the `gemini` binary |
| `MULTICA_GEMINI_MODEL` | Override the Gemini model used |
| `MULTICA_PI_PATH` | Custom path to the `pi` binary |
| `MULTICA_PI_MODEL` | Override the Pi model used |
| `MULTICA_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `MULTICA_CURSOR_MODEL` | Override the Cursor Agent model used |
| `MULTICA_KIMI_PATH` | Custom path to the `kimi` binary |
| `MULTICA_KIMI_MODEL` | Override the Kimi model used |
| `MULTICA_KIRO_PATH` | Custom path to the `kiro-cli` binary |
| `MULTICA_KIRO_MODEL` | Override the Kiro model used |

`MULTICA_CLAUDE_ARGS` and `MULTICA_CODEX_ARGS` are parsed with POSIX shellword quoting, so values such as `--model "gpt-5.1 codex" --sandbox read-only` are split like a shell command line. Agent arguments are applied in this order: hardcoded Multica defaults, daemon-wide env defaults, then per-agent `custom_args` from the task.

### Self-Hosted Server

When connecting to a self-hosted Multica instance, the easiest approach is:

```bash
# One command — configures for localhost, authenticates, starts daemon
multica setup self-host

# Or for on-premise with custom domains:
multica setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

Or configure manually:

```bash
# Set URLs individually
multica config set server_url http://localhost:8080
multica config set app_url http://localhost:3000

# For production with TLS:
# multica config set server_url https://api.example.com
# multica config set app_url https://app.example.com

multica login
multica daemon start
```

### Profiles

Profiles let you run multiple daemons on the same machine — for example, one for production and one for a staging server.

```bash
# Set up a staging profile
multica setup self-host --profile staging --server-url https://api-staging.example.com --app-url https://staging.example.com

# Start its daemon
multica daemon start --profile staging

# Default profile runs separately
multica daemon start
```

Each profile gets its own config directory (`~/.multica/profiles/<name>/`), daemon state, health port, and workspace root.

## Workspaces

### List Workspaces

```bash
multica workspace list
```

Watched workspaces are marked with `*`. The daemon only processes tasks for watched workspaces.

### Watch / Unwatch

```bash
multica workspace watch <workspace-id>
multica workspace unwatch <workspace-id>
```

### Get Details

```bash
multica workspace get <workspace-id>
multica workspace get <workspace-id> --output json
```

### List Members

```bash
multica workspace members <workspace-id>
```

## Issues

### List Issues

```bash
multica issue list
multica issue list --status in_progress
multica issue list --priority urgent --assignee "Agent Name"
multica issue list --assignee-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
multica issue list --full-id
multica issue list --limit 20 --output json
```

Table output shows a routable issue `KEY` such as `MUL-123`; copy that key into follow-up commands like `issue get`, `issue comment list`, `issue status`, or `--parent`. Add `--full-id` when you need canonical UUIDs. Available filters: `--status`, `--priority`, `--assignee` / `--assignee-id`, `--project`, `--limit`. Use `--assignee-id <uuid>` for unambiguous filtering when names overlap.

### Get Issue

```bash
multica issue get <id>
multica issue get <id> --output json
```

### Create Issue

```bash
multica issue create --title "Fix login bug" --description "..." --priority high --assignee "Lambda"
multica issue create --title "Fix login bug" --assignee-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
```

Flags: `--title` (required), `--description`, `--status`, `--priority`, `--assignee` / `--assignee-id`, `--parent`, `--project`, `--due-date`. Pass `--assignee-id <uuid>` (mutually exclusive with `--assignee`) when scripting against the IDs returned by `multica workspace members --output json` / `multica agent list --output json`.

### Update Issue

```bash
multica issue update <id> --title "New title" --priority urgent
```

### Assign Issue

```bash
multica issue assign <id> --to "Lambda"
multica issue assign <id> --to-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
multica issue assign <id> --unassign
```

Pass `--to-id <uuid>` to assign by canonical UUID (mutually exclusive with `--to`); useful when names overlap across members and agents.

### Change Status

```bash
multica issue status <id> in_progress
```

Valid statuses: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.

### Comments

```bash
# List comments
multica issue comment list <issue-id>

# Add a comment
multica issue comment add <issue-id> --content "Looks good, merging now"

# Reply to a specific comment
multica issue comment add <issue-id> --parent <comment-id> --content "Thanks!"

# Delete a comment
multica issue comment delete <comment-id>
```

### Subscribers

```bash
# List subscribers of an issue
multica issue subscriber list <issue-id>

# Subscribe yourself to an issue
multica issue subscriber add <issue-id>

# Subscribe another member or agent by name
multica issue subscriber add <issue-id> --user "Lambda"

# Unsubscribe yourself
multica issue subscriber remove <issue-id>

# Unsubscribe another member or agent
multica issue subscriber remove <issue-id> --user "Lambda"
```

Subscribers receive notifications about issue activity (new comments, status changes, etc.). Without `--user`, the command acts on the caller.

### Execution History

```bash
# List all execution runs for an issue
multica issue runs <issue-id>
multica issue runs <issue-id> --full-id
multica issue runs <issue-id> --output json

# View messages for a specific execution run
multica issue run-messages <task-id>
multica issue run-messages <short-task-id> --issue <issue-id>
multica issue run-messages <task-id> --output json

# Incremental fetch (only messages after a given sequence number)
multica issue run-messages <task-id> --since 42 --output json
```

The `runs` command shows all past and current executions for an issue, including running tasks. Table output uses short task UUID prefixes by default; pass `--full-id` to print canonical task UUIDs. The `run-messages` command accepts full task UUIDs directly; copied short task prefixes must be scoped with `--issue <issue-id>` so the CLI only checks that issue's runs. It shows the detailed message log (tool calls, thinking, text, errors) for a single run. Use `--since` for efficient polling of in-progress runs.

## Projects

Projects group related issues (e.g. a sprint, an epic, a workstream). Every project
belongs to a workspace and can optionally have a lead (member or agent).

### List Projects

```bash
multica project list
multica project list --status in_progress
multica project list --output json
```

Available filters: `--status`.

### Get Project

```bash
multica project get <id>
multica project get <id> --output json
```

### Create Project

```bash
multica project create --title "2026 Week 16 Sprint" --icon "🏃" --lead "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--icon`, `--lead`.

### Update Project

```bash
multica project update <id> --title "New title" --status in_progress
multica project update <id> --lead "Lambda"
```

Flags: `--title`, `--description`, `--status`, `--icon`, `--lead`.

### Change Status

```bash
multica project status <id> in_progress
```

Valid statuses: `planned`, `in_progress`, `paused`, `completed`, `cancelled`.

### Delete Project

```bash
multica project delete <id>
```

### Associating Issues with Projects

Use the `--project` flag on `issue create` / `issue update` to attach an issue to a
project, or on `issue list` to filter issues by project:

```bash
multica issue create --title "Login bug" --project <project-id>
multica issue update <issue-id> --project <project-id>
multica issue list --project <project-id>
```

## Setup

```bash
# One-command setup for Multica Cloud: configure, authenticate, and start the daemon
multica setup

# For local self-hosted deployments
multica setup self-host

# Custom ports
multica setup self-host --port 9090 --frontend-port 4000

# On-premise with custom domains
multica setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

`multica setup` configures the CLI, opens your browser for authentication, and starts the daemon — all in one step. Use `multica setup self-host` to connect to a self-hosted server instead of Multica Cloud.

## Configuration

### View Config

```bash
multica config show
```

Shows config file path, server URL, app URL, and default workspace.

### Set Values

```bash
multica config set server_url https://api.example.com
multica config set app_url https://app.example.com
multica config set workspace_id <workspace-id>
```

## Autopilot Commands

Autopilots are scheduled/triggered automations that dispatch agent tasks (either by creating an issue or by running an agent directly).

### List Autopilots

```bash
multica autopilot list
multica autopilot list --full-id
multica autopilot list --status active --output json
```

Autopilot table IDs are short UUID prefixes; follow-up autopilot commands accept copied prefixes when they are unique in the current workspace. Use `--full-id` to print canonical UUIDs.

### Get Autopilot Details

```bash
multica autopilot get <id>
multica autopilot get <id> --output json   # includes triggers
```

### Create / Update / Delete

```bash
multica autopilot create \
  --title "Nightly bug triage" \
  --description "Scan todo issues and prioritize." \
  --agent "Lambda" \
  --mode create_issue

multica autopilot update <id> --status paused
multica autopilot update <id> --description "New prompt"
multica autopilot delete <id>
```

`--mode` currently only accepts `create_issue` (creates a new issue on each run and assigns it to the agent). The server data model also defines `run_only`, but the daemon task path doesn't yet resolve a workspace for runs without an issue, so it's not exposed by the CLI. `--agent` accepts either a name or UUID.

### Manual Trigger

```bash
multica autopilot trigger <id>            # Fires the autopilot once, returns the run
```

### Run History

```bash
multica autopilot runs <id>
multica autopilot runs <id> --limit 50 --output json
```

### Schedule Triggers

```bash
multica autopilot trigger-add <autopilot-id> --cron "0 9 * * 1-5" --timezone "America/New_York"
multica autopilot trigger-update <autopilot-id> <trigger-id> --enabled=false
multica autopilot trigger-delete <autopilot-id> <trigger-id>
```

Only cron-based `schedule` triggers are currently exposed via the CLI. The data model also defines `webhook` and `api` kinds, but there is no server endpoint that fires them yet, so they're not surfaced here.

## Other Commands

```bash
multica version              # Show CLI version and commit hash
multica update               # Update to latest version
multica agent list           # List agents in the current workspace
```

## Output Formats

Most commands support `--output` with two formats:

- `table` — human-readable table (default for list commands)
- `json` — structured JSON (useful for scripting and automation)

```bash
multica issue list --output json
multica daemon status --output json
```
