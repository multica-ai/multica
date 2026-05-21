# Local Development Guide

Complete guide to running Multica locally for development and agent orchestration.

## Prerequisites

- [Docker Desktop](https://www.docker.com/) (for Postgres, backend, frontend)
- [Go](https://go.dev/) 1.26+ (for building the daemon binary)
- [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/) (`brew install cloudflared`) — for GitHub webhooks
- At least one agent CLI on your PATH: `copilot`, `claude`, `codex`

## 1. Environment Setup

```bash
cd multica
cp .env.example .env
```

Edit `.env` with these settings for local development:

```env
MULTICA_LOCAL_MODE=true
MULTICA_LOCAL_EMAIL=local@localhost
NEXT_PUBLIC_LOCAL_MODE=true
PORT=8090
FRONTEND_PORT=3900
JWT_SECRET=your-secret-here
```

> **Why these ports?** Default ports (8080/3000) often conflict with other dev servers. Using 8090/3900 avoids collisions.

## 2. Start Services

```bash
# Build and start all services (Postgres, migrations, backend, frontend)
docker compose up --build -d

# Verify everything is healthy
docker compose ps
```

Expected output — all containers running:
| Service | Port | Status |
|---------|------|--------|
| postgres | 5432 | healthy |
| backend | 8090 → 8080 | healthy |
| frontend | 3900 → 3000 | healthy |

Open **http://localhost:3900** — you should see the Multica dashboard (no login required in local mode).

## 3. Build and Start the Daemon

The daemon runs on your local machine (outside Docker) and connects agent CLIs to Multica.

```bash
# Build the daemon and CLI binaries
cd server
go build -o bin/multica ./cmd/multica
go build -o bin/server ./cmd/server
go build -o bin/migrate ./cmd/migrate

# Start the daemon
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon start
```

Check the daemon is connected:

```bash
tail -20 ~/.multica/daemon.log
```

You should see:
```
INF authenticated via /auth/local-login
INF registered runtime ... provider=copilot
INF watching workspace ... runtimes=3
INF health server listening addr=127.0.0.1:19514
```

### Daemon Commands

```bash
# Check daemon status
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon status

# View logs
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon logs

# Stop the daemon
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon stop

# Restart (stop + start)
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon stop
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon start
```

> **Tip:** The daemon must be restarted after rebuilding the backend Docker container (JWT tokens change on restart).

## 4. GitHub Webhooks (Cloudflare Tunnel)

GitHub requires HTTPS for webhooks. Use Cloudflare Tunnel to expose your local backend.

### Start the Tunnel

```bash
cloudflared tunnel --url http://localhost:8090
```

This outputs a public URL like:
```
https://abc123-random-words.trycloudflare.com
```

### Create the Webhook on GitHub

#### Generate an HMAC Secret

```bash
# Generate a 256-bit secret
openssl rand -hex 32
# Example output: 07df2386a4321f2ca2bbbe5e6ae65bbcf0b7c63d5be8b2ff35aa5ef91d955dd4
```

Save this secret — you'll use it in both GitHub and Multica.

#### Add the Secret to Multica

```bash
# Replace <secret> and <workspace-id> with your values
docker exec multica-postgres-1 psql -U multica -d multica -c \
  "UPDATE workspace SET webhook_secret = '<secret>' WHERE id = '<workspace-id>';"
```

Find your workspace ID at http://localhost:3900/settings or via:
```bash
curl -s http://localhost:8090/api/workspaces -H "Authorization: Bearer <token>" | python3 -m json.tool
```

#### Create the Webhook

1. Go to your repo → **Settings → Webhooks → Add webhook**
2. **Payload URL:** `https://<tunnel-url>/api/webhooks/github/<workspace-slug>`
   - Find your workspace slug at http://localhost:3900/settings or via API
3. **Content type:** `application/json`
4. **Secret:** Paste the HMAC secret you generated above
5. **Events:** Select individual events:
   - ✅ Pull request review comments
   - ✅ Pull request reviews
   - ✅ Issue comments
6. Click **Add webhook**

Or create it via CLI:

```bash
WEBHOOK_URL="https://<tunnel-url>/api/webhooks/github/<workspace-slug>"
SECRET="<your-hmac-secret>"

gh api repos/<owner>/<repo>/hooks --method POST --input - << EOF
{
  "name": "web",
  "active": true,
  "events": ["pull_request_review_comment", "pull_request_review", "issue_comment"],
  "config": {
    "url": "$WEBHOOK_URL",
    "content_type": "json",
    "secret": "$SECRET",
    "insecure_ssl": "0"
  }
}
EOF
```

### Test the Webhook

```bash
# Send a test ping
gh api repos/<owner>/<repo>/hooks/<webhook-id>/pings --method POST

# Check the backend received it
docker logs multica-backend-1 2>&1 | grep webhook
```

> **Note:** The tunnel URL changes every time you restart `cloudflared`. Update the webhook URL in GitHub Settings after each restart. For a permanent URL, use a [named Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/tunnel-guide/) with a custom domain.

## 5. Run Migrations

When you change database schema:

```bash
# Run from the server directory
cd server
DATABASE_URL="postgres://multica:multica@localhost:5432/multica?sslmode=disable" ./bin/migrate up
```

## 6. Rebuild After Code Changes

### Backend changes (Go)

```bash
# Rebuild binaries
cd server && go build -o bin/multica ./cmd/multica && go build -o bin/server ./cmd/server

# Rebuild Docker and restart
cd .. && docker compose build backend && docker compose up -d backend

# Restart daemon (picks up new JWT)
cd server
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon stop
MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon start
```

### Frontend changes (Next.js)

```bash
docker compose build frontend && docker compose up -d frontend
```

> `NEXT_PUBLIC_*` env vars are baked at build time. If you change them, you must rebuild the frontend image.

## 7. Troubleshooting

### "not authenticated" error on daemon start
The daemon needs `MULTICA_LOCAL_MODE=true` and `MULTICA_SERVER_URL=http://localhost:8090` as env vars.

### Daemon claim errors (401 invalid token)
The backend was restarted and JWT secret changed. Restart the daemon.

### Frontend shows login screen
`NEXT_PUBLIC_LOCAL_MODE=true` must be set during Docker build. Rebuild: `docker compose build frontend`

### WebSocket not connecting (no live updates)
The WS URL is derived from `window.location`. If you access via a different host/port than expected, check the browser console for WebSocket errors.

### Agents not picking up tasks
1. Check daemon is running: `tail ~/.multica/daemon.log`
2. Check agent status in UI: should show "Idle", not "Offline"
3. If status is "Working" but agent isn't running, cancel the stuck task from the UI

### Tunnel URL changed
Update the GitHub webhook URL: **Repo → Settings → Webhooks → Edit → Payload URL**

## Quick Reference

| Action | Command |
|--------|---------|
| Start everything | `docker compose up -d && cd server && MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon start` |
| Stop everything | `cd server && MULTICA_LOCAL_MODE=true MULTICA_SERVER_URL=http://localhost:8090 ./bin/multica daemon stop && cd .. && docker compose down` |
| View daemon logs | `tail -f ~/.multica/daemon.log` |
| View backend logs | `docker logs -f multica-backend-1` |
| Start tunnel | `cloudflared tunnel --url http://localhost:8090` |
| Open UI | http://localhost:3900 |
| API health | http://localhost:8090/health |
