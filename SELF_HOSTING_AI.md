# Self-Hosting Setup (for AI Agents)

This document is designed for AI agents to execute. Follow these steps exactly to deploy a local Multica instance and connect to it.

## Prerequisites

- Docker and Docker Compose installed
- Homebrew installed (for CLI)
- At least one AI agent CLI on PATH: `claude` or `codex`

## One-Command Install

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --local
```

This installs everything: clones the repo, starts Docker services, and installs the CLI.

Wait for the output `✓ Multica is installed and running!` before proceeding.

**Expected result:**
- Frontend at http://localhost:3000
- Backend at http://localhost:8080
- `multica` CLI installed and configured for localhost

## Alternative: Manual Setup

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
make selfhost
brew install multica-ai/tap/multica
multica setup --local
```

The `multica setup --local` command will:
1. Configure CLI to connect to localhost:8080 / localhost:3000
2. Open a browser for login — use verification code `888888` with any email
3. Discover workspaces automatically
4. Start the daemon in the background

## Verification

```bash
multica daemon status
```

Should show `running` with detected agents.

## Stopping

```bash
# Stop the daemon
multica daemon stop

# Stop all Docker services
cd multica
make selfhost-stop
```

## Custom Ports

If the default ports (8080/3000) are in use:

1. Edit `.env` and change `PORT` and `FRONTEND_PORT`
2. Run `make selfhost`
3. Run `multica setup --local --port <PORT> --frontend-port <FRONTEND_PORT>`

## Troubleshooting

- **Backend not ready:** `docker compose -f docker-compose.selfhost.yml logs backend`
- **Frontend not ready:** `docker compose -f docker-compose.selfhost.yml logs frontend`
- **Daemon issues:** `multica daemon logs`
- **Health check:** `curl http://localhost:8080/health`
