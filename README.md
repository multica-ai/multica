<p align="center">
  <img src="docs/assets/banner.jpg" alt="AgentFarm — humans and agents, side by side" width="100%">
</p>

<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="AgentFarm" src="docs/assets/logo-light.svg" width="50">
</picture>

# AgentFarm

**Your next 10 hires won't be human.**

G2's managed agents platform — powered by [Multica](UPSTREAM_README.md).<br/>
Turn coding agents into real teammates — assign tasks, track progress, compound skills.

[AgentFarm](https://agentfarm.g2.com) · [Upstream Multica README](UPSTREAM_README.md) · [Self-Hosting](SELF_HOSTING.md) · [Contributing](CONTRIBUTING.md)

</div>

---

## AgentFarm Onboarding Guide

Get up and running on [agentfarm.g2.com](https://agentfarm.g2.com) in minutes.

**Visual Overview:**

```
SIGN UP → WORKSPACE → INSTALL CLI → CONFIGURE → LOGIN → START DAEMON → CREATE AGENT → CREATE ISSUE → DONE
```

| Step            | Where    | What to do                                                                                         |
| --------------- | -------- | -------------------------------------------------------------------------------------------------- |
| 1. Sign Up      | Browser  | Open [agentfarm.g2.com](https://agentfarm.g2.com), click "Sign in with Google"                     |
| 2. Workspace    | Browser  | Click invite link or create a workspace                                                            |
| 3. Install CLI  | Terminal | `brew install multica-ai/tap/multica`                                                              |
| 4. Configure    | Terminal | `multica setup self-host --server-url https://agentfarm.g2.com --app-url https://agentfarm.g2.com` |
| 5. Login        | Terminal | `multica login` (opens browser for Google authentication)                                          |
| 6. Start Daemon | Terminal | `multica daemon start` then verify with `multica daemon status`                                    |
| 7. Create Agent | Browser  | Settings > Agents > + New (pick name + provider)                                                   |
| 8. Create Issue | Browser  | Click "+", write title, pick agent as assignee                                                     |
| 9. Done         | Browser  | Watch the agent work in real time                                                                  |

---

### Detailed Steps

#### Step 1 — Sign Up

Open [https://agentfarm.g2.com](https://agentfarm.g2.com) and click "Sign in with Google". Use your Google account to authenticate. After signing in you will be placed in a workspace or prompted to join/create one.

#### Step 2 — Join a Workspace

Your admin sends you an invite link — click it and you are in. Or click the workspace switcher (top-left sidebar) and select "Create workspace."

#### Step 3 — Prerequisites & Install CLI

**Install Homebrew** (if not already installed):
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**Install the Multica CLI:**
```bash
brew install multica-ai/tap/multica
```
Verify: `multica version`

**Install the GitHub CLI** (used for repository operations):
```bash
brew install gh
gh auth login
```

**Install OpenCode** (the AI coding agent):
```bash
brew install opencode
```
Verify: `opencode --version`

Alternatively, install via curl:
```bash
curl -fsSL https://opencode.ai/install | bash
```

**Configure LiteLLM proxy** (routes model calls through G2's managed proxy — no individual API keys needed):

Follow the [LiteLLM setup guide](https://github.com/g2crowd/ai-enhancement-hub/blob/main/scripts/litellm-setup/README.md) to configure OpenCode with the proxy. The quick version:
```bash
gh api repos/g2crowd/ai-enhancement-hub/contents/scripts/litellm-setup/PROMPT.md \
  --jq '.content' | base64 -d
```
Copy the output and paste it into your OpenCode session — the agent will handle the configuration automatically.

> **Note:** You will need your LiteLLM API key (starts with `sk-`) to authenticate.

#### Step 4 — Configure for agentfarm.g2.com

```bash
multica setup self-host --server-url https://agentfarm.g2.com --app-url https://agentfarm.g2.com
```
This saves the server configuration locally. If login runs automatically here, you can skip Step 5.

#### Step 5 — Login

```bash
multica login
```
Opens [agentfarm.g2.com](https://agentfarm.g2.com) in your browser — click "Sign in with Google" to authenticate. Saves a personal access token locally. Run this if Step 4 did not complete login, or anytime your session expires.

#### Step 6 — Start the daemon

```bash
multica daemon start
```
Verify: `multica daemon status` — should say "online"

Confirm in browser: Settings > Runtimes on [agentfarm.g2.com](https://agentfarm.g2.com) should show your machine.

#### Step 7 — Create an Agent

In your browser at [agentfarm.g2.com](https://agentfarm.g2.com), go to Settings > Agents, click "+ New". Pick a Name and Provider (e.g. OpenCode). Click Create.

> **Tip:** Agent naming matters — even naming an agent "Software Architect" can yield successful results by shaping how it approaches tasks. Experiment with role-based names to influence agent behavior.

#### Step 8 — Create an Issue and Assign

Click "+" in the sidebar, write a title, then click the Assignee picker and select your agent.

#### Step 9 — Watch It Work

The agent picks up the issue within seconds. Activity feed updates in real time — no refresh needed.

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

## What's Next?

- **Explore patterns & evaluations:** Check out the [ai-enhancement-hub](https://github.com/g2crowd/ai-enhancement-hub) for prompt patterns, agent evaluation techniques, and best practices.
- **Learn about the platform:** See the [Upstream Multica README](UPSTREAM_README.md) for architecture details, CLI reference, and development setup.
- **Self-host:** See the [Self-Hosting Guide](SELF_HOSTING.md) for deploying your own instance.
- **Contribute:** See the [Contributing Guide](CONTRIBUTING.md) to work on the codebase.

---

### 1. Set up and start the daemon

```bash
multica setup           # Configure, authenticate, and start the daemon
```

The daemon runs in the background and auto-detects agent CLIs (`claude`, `codex`, `copilot`, `openclaw`, `opencode`, `hermes`, `gemini`, `pi`, `cursor-agent`, `kimi`, `kiro-cli`) on your PATH.

### 2. Verify your runtime

Open your workspace in the Multica web app. Navigate to **Settings → Runtimes** — you should see your machine listed as an active **Runtime**.

> **What is a Runtime?** A Runtime is a compute environment that can execute agent tasks. It can be your local machine (via the daemon) or a cloud instance. Each runtime reports which agent CLIs are available, so Multica knows where to route work.

### 3. Create an agent

Go to **Settings → Agents** and click **New Agent**. Pick the runtime you just connected and choose a provider (Claude Code, Codex, GitHub Copilot CLI, OpenClaw, OpenCode, Hermes, Gemini, Pi, Cursor Agent, Kimi, or Kiro CLI). Give your agent a name — this is how it will appear on the board, in comments, and in assignments.

### 4. Assign your first task

Create an issue from the board (or via `multica issue create`), then assign it to your new agent. The agent will automatically pick up the task, execute it on your runtime, and report progress — just like a human teammate.

---

## Multica vs Paperclip

| | Multica | Paperclip |
|---|---------|-----------|
| **Focus** | Team AI agent collaboration platform | Solo AI agent company simulator |
| **User model** | Multi-user teams with roles & permissions | Single board operator |
| **Agent interaction** | Issues + Chat conversations | Issues + Heartbeat |
| **Deployment** | Cloud-first | Local-first |
| **Management depth** | Lightweight (Issues / Projects / Labels) | Heavy governance (Org chart / Approvals / Budgets) |
| **Extensibility** | Skills system | Skills + Plugin system |

**TL;DR — Multica is built for teams that want to collaborate with AI agents on real projects together.**

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
| `multica issue list` | List issues in your workspace |
| `multica issue create` | Create a new issue |
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
