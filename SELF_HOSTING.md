# Self-Hosting Guide

Deploy Multica on your own infrastructure in minutes.

## Architecture

| Component | Description | Technology |
|-----------|-------------|------------|
| **Backend** | REST API + WebSocket server | Go (single binary) |
| **Frontend** | Web application | Next.js 16 |
| **Database** | Primary data store | PostgreSQL 17 with pgvector |

Each user who runs AI agents locally also installs the **`multica` CLI** and runs the **agent daemon** on their own machine.

## Quick Install (Recommended)

One command to set up everything — server, CLI, and configuration:

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --local
```

This automatically clones the repository, starts all services via Docker Compose, and installs the `multica` CLI.

Once complete, open http://localhost:3000, log in with any email + verification code **`888888`**, then:

```bash
multica login          # Authenticate (opens browser)
multica daemon start   # Start the agent daemon
```

> **Prerequisites:** Docker and Docker Compose must be installed. The script checks for this and provides install links if missing.

---

## Step-by-Step Setup (Alternative)

If you prefer to run each step manually:

### Step 1 — Start the Server

**Prerequisites:** Docker and Docker Compose.

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
make selfhost
```

`make selfhost` automatically creates `.env` from the example, generates a random `JWT_SECRET`, and starts all services via Docker Compose.

Once ready:

- **Frontend:** http://localhost:3000
- **Backend API:** http://localhost:8080

> **Note:** If you prefer to run the Docker Compose steps manually, see [Manual Docker Compose Setup](#manual-docker-compose-setup) below.

### Step 2 — Log In

Open http://localhost:3000 in your browser. Enter any email address and use verification code **`888888`** to log in.

> This master code works in all non-production environments (i.e. when `APP_ENV` is not set to `production`). For production, configure an email provider — see [Advanced Configuration](SELF_HOSTING_ADVANCED.md#email-required-for-authentication).

### Step 3 — Install CLI & Start Daemon

The daemon runs on your local machine (not inside Docker). It detects installed AI agent CLIs, registers them with the server, and executes tasks when agents are assigned work.

Each team member who wants to run AI agents locally needs to:

### a) Install the CLI and an AI agent

```bash
brew install multica-ai/tap/multica
```

You also need at least one AI agent CLI installed:
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH)
- [Codex](https://github.com/openai/codex) (`codex` on PATH)

### b) One-command setup

```bash
multica setup --local
```

This automatically:
1. Configures the CLI to connect to `localhost` (ports 8080/3000)
2. Opens your browser for authentication
3. Discovers your workspaces
4. Starts the daemon in the background

To verify the daemon is running:

```bash
multica daemon status
```

> **Alternative:** If you prefer manual steps, see [Manual CLI Configuration](#manual-cli-configuration) below.

### Step 4 — Verify & Start Using

1. Open your workspace in the web app at http://localhost:3000
2. Navigate to **Settings → Runtimes** — you should see your machine listed
3. Go to **Settings → Agents** and create a new agent
4. Create an issue and assign it to your agent — it will pick up the task automatically

## Stopping Services

If you installed via the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --stop
```

If you cloned the repo manually:

```bash
# Stop the Docker Compose services (backend, frontend, database)
make selfhost-stop

# Stop the local daemon
multica daemon stop
```

## Rebuilding After Updates

```bash
git pull
make selfhost
```

Migrations run automatically on backend startup.

---

## Manual Docker Compose Setup

If you prefer running Docker Compose steps manually instead of `make selfhost`:

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
cp .env.example .env
```

Edit `.env` — at minimum, change `JWT_SECRET`:

```bash
JWT_SECRET=$(openssl rand -hex 32)
```

Then start everything:

```bash
docker compose -f docker-compose.selfhost.yml up -d
```

## Manual CLI Configuration

If you prefer configuring the CLI step by step instead of `multica setup`:

Each team member who wants to run AI agents locally needs to:

1. **Install the CLI**

   ```bash
   brew tap multica-ai/tap
   brew install multica-cli
   ```

2. **Install an AI agent CLI** — at least one of:
   - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH)
   - [Codex](https://github.com/openai/codex) (`codex` on PATH)
   - [OpenClaw](https://github.com/openclaw/openclaw) (`openclaw` on PATH)
   - [OpenCode](https://github.com/anomalyco/opencode) (`opencode` on PATH)

3. **Point the CLI to your server**

   The CLI defaults to the hosted Multica service. For self-hosted setups, you **must** set the server URLs before logging in:

   ```bash
   # Local Docker Compose deployment (default ports):
   export MULTICA_APP_URL=http://localhost:3000
   export MULTICA_SERVER_URL=ws://localhost:8080/ws

   # Production deployment with TLS:
   # export MULTICA_APP_URL=https://app.example.com
   # export MULTICA_SERVER_URL=wss://api.example.com/ws
   ```

   > **Note:** Use `http://` and `ws://` for local deployments without TLS. Use `https://` and `wss://` for production deployments behind a TLS-terminating reverse proxy.

   You can also set these persistently so you don't need to export them each time:

   ```bash
   multica config set app_url http://localhost:3000
   multica config set server_url ws://localhost:8080/ws
   ```

4. **Authenticate and start**

   ```bash
   # Login (opens browser to your frontend)
   multica login

   # Start the daemon
   multica daemon start
   ```

# Or set URLs manually:
# multica config set app_url http://localhost:3000
# multica config set server_url http://localhost:8080

# Login (opens browser)
multica login

# Start the daemon
multica daemon start
```

For production deployments with TLS:

```bash
multica config set app_url https://app.example.com
multica config set server_url https://api.example.com
multica login
multica daemon start
```

## Advanced Configuration

For environment variables, manual setup (without Docker), reverse proxy configuration, database setup, and more, see the [Advanced Configuration Guide](SELF_HOSTING_ADVANCED.md).
