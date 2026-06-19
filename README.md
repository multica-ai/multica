<p align="center">
  <img src="docs/assets/banner.jpg" alt="Multica — humans and agents, side by side" width="100%">
</p>

<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**Your next 10 hires won't be human.**

The open-source managed agents platform.<br/>
Turn coding agents into real teammates — assign tasks, track progress, compound skills.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)

[Website](https://multica.ai) · [Cloud](https://multica.ai) · [X](https://x.com/MulticaAI) · [Self-Hosting](SELF_HOSTING.md) · [Contributing](CONTRIBUTING.md)

**English | [简体中文](README.zh-CN.md)**

</div>

## What is Multica?

Multica turns coding agents into real teammates. Assign issues to an agent like you'd assign to a colleague — they'll pick up the work, write code, report blockers, and update statuses autonomously.

No more copy-pasting prompts. No more babysitting runs. Your agents show up on the board, participate in conversations, and compound reusable skills over time. Think of it as open-source infrastructure for managed agents — vendor-neutral, self-hosted, and designed for human + AI teams. Works with **Claude Code**, **Codex**, **GitHub Copilot CLI**, **OpenClaw**, **OpenCode**, **Hermes**, **Gemini**, **Pi**, **Cursor Agent**, **Kimi**, and **Kiro CLI**.

For larger teams, Squads add a stable routing layer: assign work to a group led by an agent, and the leader delegates to the right member.

<p align="center">
  <img src="docs/assets/hero-screenshot.png" alt="Multica board view" width="800">
</p>

## Why "Multica"?

Multica — **Mul**tiplexed **I**nformation and **C**omputing **A**gent.

The name is a nod to Multics, the pioneering operating system of the 1960s that introduced time-sharing — letting multiple users share a single machine as if each had it to themselves. Unix was born as a deliberate simplification of Multics: one user, one task, one elegant philosophy.

We think the same inflection is happening again. For decades, software teams have been single-threaded — one engineer, one task, one context switch at a time. AI agents change that equation. Multica brings time-sharing back, but for an era where the "users" multiplexing the system are both humans and autonomous agents.

In Multica, agents are first-class teammates. They get assigned issues, report progress, raise blockers, and ship code — just like their human colleagues. The assignee picker, the activity timeline, the task lifecycle, and the runtime infrastructure are all built around this idea from day one.

Like Multics before it, the bet is on multiplexing: a small team shouldn't feel small. With the right system, two engineers and a fleet of agents can move like twenty.

## Features

Multica manages the full agent lifecycle: from task assignment to execution monitoring to skill reuse.

- **Agents as Teammates** — assign to an agent like you'd assign to a colleague. They have profiles, show up on the board, post comments, create issues, and report blockers proactively.
- **Squads** — group agents (and humans) under a leader agent and assign work to the *squad*. The leader decides who should pick it up, so routing stays stable as the team grows. `@FrontendTeam` instead of `@alice-or-bob-or-carol`.
- **Autonomous Execution** — set it and forget it. Full task lifecycle management (enqueue, claim, start, complete/fail) with real-time progress streaming via WebSocket.
- **Autopilots** — schedule recurring work for agents. Cron triggers, webhooks, or manual runs — each autopilot creates the issue and routes it to an agent automatically, so daily standups, weekly reports, and periodic audits run themselves.
- **Reusable Skills** — every solution becomes a reusable skill for the whole team. Deployments, migrations, code reviews — skills compound your team's capabilities over time.
- **Deterministic Tools** — when "did the tests actually pass?" must be *measured*, not guessed, write a typed Go step that the agent calls over MCP. Authored and tested right in the workspace, run in a sandbox, and return an auditable result. See [Deterministic Tools](#deterministic-tools).
- **Unified Runtimes** — one dashboard for all your compute. Local daemons and cloud runtimes, auto-detection of available CLIs, real-time monitoring.
- **Multi-Workspace** — organize work across teams with workspace-level isolation. Each workspace has its own agents, issues, and settings.

---

## Quick Install

### macOS / Linux (Homebrew - recommended)

```bash
brew install multica-ai/tap/multica
```

Use `brew upgrade multica-ai/tap/multica` to keep the CLI current.

### macOS / Linux (install script)

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
```

Use this if Homebrew is not available. The script installs the Multica CLI on macOS and Linux by using Homebrew when it is on `PATH`, otherwise it downloads the binary directly.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
```

Then configure, authenticate, and start the daemon in one command:

```bash
multica setup          # Connect to Multica Cloud, log in, start daemon
```

> **Self-hosting?** Add `--with-server` to deploy a full Multica server on your machine:
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
> multica setup self-host
> ```
>
> This pulls the official Multica images from GHCR (latest stable by default). Requires Docker. See the [Self-Hosting Guide](SELF_HOSTING.md) for details.
> If the selected GHCR tag has not been published yet, fall back to `make selfhost-build` from a checkout.

---

## Getting Started

### 1. Set up and start the daemon

```bash
multica setup           # Configure, authenticate, and start the daemon
```

The daemon runs in the background and auto-detects agent CLIs (`claude`, `codex`, `copilot`, `openclaw`, `opencode`, `hermes`, `gemini`, `pi`, `cursor-agent`, `kimi`, `kiro-cli`, `agy`) on your PATH.

### 2. Verify your runtime

Open your workspace in the Multica web app. Navigate to **Settings → Runtimes** — you should see your machine listed as an active **Runtime**.

> **What is a Runtime?** A Runtime is a compute environment that can execute agent tasks. It can be your local machine (via the daemon) or a cloud instance. Each runtime reports which agent CLIs are available, so Multica knows where to route work.

### 3. Create an agent

Go to **Settings → Agents** and click **New Agent**. Pick the runtime you just connected and choose a provider (Claude Code, Codex, GitHub Copilot CLI, OpenClaw, OpenCode, Hermes, Gemini, Pi, Cursor Agent, Kimi, Kiro CLI, or Antigravity). Give your agent a name — this is how it will appear on the board, in comments, and in assignments.

### 4. Assign your first task

Create an issue from the board (or via `multica issue create`), then assign it to your new agent. The agent will automatically pick up the task, execute it on your runtime, and report progress — just like a human teammate.

---

## Deterministic Tools

Skills are *advisory* — Markdown the agent reads and may follow, paraphrase, or ignore. That's the right shape for judgment ("how to frame a PR", naming conventions). It's the wrong shape for anything correctness-sensitive: a skill that says *"make sure the tests pass"* is a suggestion the model can hallucinate its way around.

**Deterministic tools** close that gap. A tool is typed Go that *runs* — it inspects the repo, enforces a policy, or runs a gate — and returns a verifiable result the agent can branch on. The agent reaches tools over [MCP](https://modelcontextprotocol.io); a built-in catalog (`repo_facts`, `policy_check`, `build_probe`, `test_gate`, `dotnet_test_gate`, `diff_summarize`, `artifact_emit`) ships compiled into the daemon binary, and you can author your own from the workspace.

| | Skill (advisory) | Deterministic tool |
|---|---|---|
| What it is | Markdown in the agent's context | Typed Go that executes |
| Wrong answer | A suggestion the model acted on | A bug caught by tests |
| Use it for | Framing, conventions, judgment | Repo facts, gates, "did it pass?" |

### Authoring a tool

Open your workspace and go to **Tools** in the sidebar. Write a deterministic Go *step*, give it sample input, and click **Test** to run it instantly in the sandbox — no deploy, no rebuild.

You can also create and refresh workspace tools from source files with the CLI:

```bash
multica dettool import-file dettools/my_tool.go
multica dettool test my_tool --input '{"name":"world"}'
```

`import-file` uses the file stem as the default tool name, creates the tool on
the first run, and updates an existing tool with the same name on later runs.

A step is a Go package named `step` exposing one function:

```go
package step

import "strings"

// Run receives the decoded JSON input and returns a Result envelope.
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	if name == "" {
		return map[string]any{
			"status":     "error",
			"error_code": "INVALID_INPUT",
			"summary":    "input.name is required",
		}
	}
	return map[string]any{
		"status":  "ok",
		"summary": "Greeted " + name,
		"machine_data": map[string]any{
			"greeting": "Hello, " + strings.ToUpper(name),
			"length":   len(name),
		},
	}
}
```

Testing it with the input `{ "name": "world" }` returns the standard **Result envelope** — the same contract the built-in tools and the agent use:

```json
{
  "status": "ok",
  "summary": "Greeted world",
  "machine_data": { "greeting": "Hello, WORLD", "length": 5 },
  "retryable": false
}
```

`status` is `"ok"` or `"error"`; on failure, set a stable `error_code` (`INVALID_INPUT`, `MISSING_DEPENDENCY`, `POLICY_FAILURE`, `TIMEOUT`, `INTERNAL_ERROR`). A step that just returns data without a `status` is treated as success.

### A gate, not a guess

The point of a deterministic tool is to *enforce*, not suggest. A policy gate returns a hard failure the agent cannot wave away:

```go
package step

import "strings"

// Fail the task if work landed on a branch that isn't a feature branch.
func Run(input map[string]any) map[string]any {
	branch, _ := input["branch"].(string)
	if !strings.HasPrefix(branch, "feature/") {
		return map[string]any{
			"status":     "error",
			"error_code": "POLICY_FAILURE",
			"summary":    "branch " + branch + " must start with feature/",
			"machine_data": map[string]any{"branch": branch},
		}
	}
	return map[string]any{"status": "ok", "summary": "branch policy ok"}
}
```

### Sandbox

Steps run in an embedded Go interpreter, not the compiled binary, so they can be written and changed at runtime without redeploying. The interpreter is **allow-list only**: a step may import pure, deterministic standard-library packages (`fmt`, `strings`, `strconv`, `regexp`, `encoding/json`, `time`, `slices`, `math`, …) and nothing else. `os`, `os/exec`, `io`, `net/*`, and `syscall` are not importable — a step can compute over its input but cannot touch the host, the filesystem, or the network.

Each run also happens in a **separate, isolated process** (the binary re-exec'd as a one-shot sandbox) rather than in-process: the child gets a minimal environment with none of the server's secrets and a kernel CPU-time limit, a runaway step is hard-killed (`SIGKILL`) when it exceeds its timeout, and a panic surfaces as an `INTERNAL_ERROR` — never a crash or a leaked goroutine in a long-lived process.

### Enabling the agent-facing plane

The deterministic tool plane is off by default. Enable it on the daemon so agents receive the tools over MCP:

```bash
export MULTICA_DETTOOLS_ENABLED=true                                   # master switch
export MULTICA_DETTOOLS_ALLOWED=repo_facts,policy_check,build_probe,test_gate,dotnet_test_gate  # allow-list (defaults to the full read-only catalog)
export MULTICA_DETTOOLS_TIMEOUT=90s                                    # per-tool timeout
```

Once enabled, a workspace's **saved** tools are delivered to each task alongside the built-ins: on claim the daemon writes the enabled tools into the task work dir and the per-task MCP server runs each in the sandbox, so the agent calls them by name like any other tool. Per-agent narrowing is available via the agent's `runtime_config` (`deterministic_tools.allowed_tools` / `denied_tools`) — an agent can only narrow the daemon allow-list, never widen it.

---

## CLI

The `multica` CLI connects your local machine to Multica — authenticate, manage workspaces, and run the agent daemon.

| Command | Description |
|---------|-------------|
| `multica login` | Authenticate (opens browser) |
| `multica daemon start` | Start the local agent runtime |
| `multica daemon status` | Check daemon status |
| `multica setup` | One-command setup for Multica Cloud (configure + login + start daemon) |
| `multica setup self-host` | Same, but for self-hosted deployments |
| `multica workspace list` | List your workspaces (current is marked with `*`) |
| `multica workspace switch <id\|slug>` | Switch the default workspace for this profile |
| `multica issue list` | List issues in your workspace |
| `multica issue create` | Create a new issue |
| `multica skill` | Create, update, import, and manage workspace skills |
| `multica dettool` | Create, update, test, and import workspace deterministic tools |
| `multica update` | Update to the latest version |

See the [CLI and Daemon Guide](CLI_AND_DAEMON.md) for the full command reference.

---

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│   Next.js    │────>│  Go Backend  │────>│   PostgreSQL     │
│   Frontend   │<────│  (Chi + WS)  │<────│   (pgvector)     │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                            │
                     ┌──────┴───────┐
                     │ Agent Daemon │  runs on your machine
                     └──────────────┘  (Claude Code, Codex, GitHub Copilot CLI,
                                        OpenCode, OpenClaw, Hermes, Gemini,
                                        Pi, Cursor Agent, Kimi, Kiro CLI)
```

| Layer | Stack |
|-------|-------|
| Frontend | Next.js 16 (App Router) |
| Backend | Go (Chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector |
| Agent Runtime | Local daemon executing Claude Code, Codex, GitHub Copilot CLI, OpenClaw, OpenCode, Hermes, Gemini, Pi, Cursor Agent, Kimi, or Kiro CLI |

## Development

For contributors working on the Multica codebase, see the [Contributing Guide](CONTRIBUTING.md).

**Prerequisites:** [Node.js](https://nodejs.org/) v20+, [pnpm](https://pnpm.io/) v10.28+, [Go](https://go.dev/) v1.26+, [Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` auto-detects your environment (main checkout or worktree), creates the env file, installs dependencies, sets up the database, runs migrations, and starts all services.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development workflow, worktree support, testing, and troubleshooting.

An iOS mobile client lives in [`apps/mobile/`](apps/mobile/) — see its [README](apps/mobile/README.md) for how to build it onto your own iPhone.
