# Self-Hosting — Advanced Configuration

This document covers advanced configuration for self-hosted Multica deployments. For the quick start guide, see [SELF_HOSTING.md](SELF_HOSTING.md).

## Configuration

All configuration is done via environment variables. Copy `.env.example` as a starting point.

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://multica:multica@localhost:5432/multica?sslmode=disable` |
| `JWT_SECRET` | **Must change from default.** Secret key for signing JWT tokens. Use a long random string. | `openssl rand -hex 32` |
| `FRONTEND_ORIGIN` | URL where the frontend is served (used for CORS) | `https://app.example.com` |

### Database Pool Tuning (Optional)

These have sensible defaults and only need to be set when tuning a large or constrained deployment. Precedence (highest first): env var → `pool_*` query params on `DATABASE_URL` → built-in default.

| Variable | Description | Default |
|----------|-------------|---------|
| `DATABASE_MAX_CONNS` | pgxpool max connections per pod. `pod_count × DATABASE_MAX_CONNS` should stay well below the Postgres `max_connections` ceiling. With a connection pooler (PgBouncer / RDS Proxy / Supavisor) in front, this can be raised significantly. | `25` |
| `DATABASE_MIN_CONNS` | pgxpool warm baseline connections per pod. Auto-clamped to `DATABASE_MAX_CONNS`. | `5` |

### Email (Required for Authentication)

Multica uses email-based magic link authentication via [Resend](https://resend.com).

| Variable | Description |
|----------|-------------|
| `RESEND_API_KEY` | Your Resend API key |
| `RESEND_FROM_EMAIL` | Sender email address (default: `noreply@multica.ai`) |

> **Note:** The dev master verification code `888888` is gated by `APP_ENV != "production"`. The Docker self-host stack defaults to `APP_ENV=production` (so `888888` is disabled), which protects publicly reachable instances. For local development without email configured, set `APP_ENV=development` in your `.env` to enable `888888` — never do this on a public instance.

### Google OAuth (Optional)

| Variable | Description |
|----------|-------------|
| `GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret |
| `GOOGLE_REDIRECT_URI` | OAuth callback URL (e.g. `https://app.example.com/auth/callback`) |

### File Storage (Optional)

For file uploads and attachments, configure S3 and CloudFront:

| Variable | Description |
|----------|-------------|
| `S3_BUCKET` | S3 bucket name |
| `S3_REGION` | AWS region (default: `us-west-2`) |
| `CLOUDFRONT_DOMAIN` | CloudFront distribution domain |
| `CLOUDFRONT_KEY_PAIR_ID` | CloudFront key pair ID for signed URLs |
| `CLOUDFRONT_PRIVATE_KEY` | CloudFront private key (PEM format) |

### Cookies

| Variable | Description |
|----------|-------------|
| `COOKIE_DOMAIN` | Optional `Domain` attribute for session + CloudFront cookies. **Leave empty** for single-host deployments (localhost, LAN IP, or a single hostname). Only set it when the frontend and backend sit on different subdomains of one registered domain (e.g. `.example.com`). **Do not use an IP literal** — RFC 6265 forbids IP addresses in the cookie `Domain` attribute and browsers will drop such `Set-Cookie` headers. |

The `Secure` flag on session cookies is derived automatically from the scheme of `FRONTEND_ORIGIN`: HTTPS origins get `Secure` cookies; plain-HTTP origins (LAN / private-network self-host) get non-secure cookies so the browser can actually store them.

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend server port |
| `FRONTEND_PORT` | `3000` | Frontend port |
| `CORS_ALLOWED_ORIGINS` | Value of `FRONTEND_ORIGIN` | Comma-separated list of allowed origins |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### CLI / Daemon

These are configured on each user's machine, not on the server:

| Variable | Default | Description |
|----------|---------|-------------|
| `MULTICA_SERVER_URL` | `ws://localhost:8080/ws` | WebSocket URL for daemon → server connection |
| `MULTICA_APP_URL` | `http://localhost:3000` | Frontend URL for CLI login flow |
| `MULTICA_DAEMON_POLL_INTERVAL` | `3s` | How often the daemon polls for tasks |
| `MULTICA_DAEMON_HEARTBEAT_INTERVAL` | `15s` | Heartbeat frequency |

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `MULTICA_CLAUDE_PATH` | Custom path to the `claude` binary |
| `MULTICA_CLAUDE_MODEL` | Override the Claude model used |
| `MULTICA_CODEX_PATH` | Custom path to the `codex` binary |
| `MULTICA_CODEX_MODEL` | Override the Codex model used |
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

## Database Setup

Multica requires PostgreSQL 17 with the pgvector extension.

### Using Docker Compose (Recommended)

The `docker-compose.selfhost.yml` includes PostgreSQL. No separate setup needed.

### Using Your Own PostgreSQL

If you prefer to use an existing PostgreSQL instance, ensure the pgvector extension is available:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

Set `DATABASE_URL` in your `.env` and remove the `postgres` service from the compose file.

### Running Migrations Manually

The Docker Compose setup runs migrations automatically. If you need to run them manually:

```bash
# Using the built binary
./server/bin/migrate up

# Or from source
cd server && go run ./cmd/migrate up
```

## Manual Setup (Without Docker Compose)

If you prefer to build and run services manually:

**Prerequisites:** Go 1.26+, Node.js 20+, pnpm 10.28+, PostgreSQL 17 with pgvector.

```bash
# Start your PostgreSQL (or use: docker compose up -d postgres)

# Build the backend
make build

# Run database migrations
DATABASE_URL="your-database-url" ./server/bin/migrate up

# Start the backend server
DATABASE_URL="your-database-url" PORT=8080 JWT_SECRET="your-secret" ./server/bin/server
```

For the frontend:

```bash
pnpm install
pnpm build

# Start the frontend (production mode)
cd apps/web
REMOTE_API_URL=http://localhost:8080 pnpm start
```

## Reverse Proxy

In production, put a reverse proxy in front of both the backend and frontend to handle TLS and routing.

### Caddy (Recommended)

```
app.example.com {
    reverse_proxy localhost:3000
}

api.example.com {
    reverse_proxy localhost:8080
}
```

### Nginx

```nginx
# Frontend
server {
    listen 443 ssl;
    server_name app.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# Backend API
server {
    listen 443 ssl;
    server_name api.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket support
    location /ws {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400;
    }
}
```

When using separate domains for frontend and backend, set these environment variables accordingly:

```bash
# Backend
FRONTEND_ORIGIN=https://app.example.com
CORS_ALLOWED_ORIGINS=https://app.example.com

# Frontend (set before building the frontend image)
REMOTE_API_URL=https://api.example.com
NEXT_PUBLIC_API_URL=https://api.example.com
NEXT_PUBLIC_WS_URL=wss://api.example.com/ws
```

## LAN / Non-localhost Access

By default, Multica works on `localhost`. If you access it from another machine on the LAN (e.g. `http://192.168.1.100:3000`), you need to tell the backend to accept that origin:

```bash
# .env — replace with your server's LAN IP
FRONTEND_ORIGIN=http://192.168.1.100:3000
CORS_ALLOWED_ORIGINS=http://192.168.1.100:3000
```

Then rebuild:

```bash
docker compose -f docker-compose.selfhost.yml up -d --build
```

The frontend automatically derives the WebSocket URL from the page address, so real-time features (chat streaming, live issue updates, notifications) work over LAN without extra configuration.

> **Note:** If you need to override the WebSocket URL explicitly (e.g. when using a separate backend domain), set `NEXT_PUBLIC_WS_URL` in `.env` and rebuild the frontend image.

## Health Check

The backend exposes a health check endpoint:

```
GET /health
→ {"status":"ok"}
```

Use this for load balancer health checks or monitoring.

## Upgrading

```bash
git pull
docker compose -f docker-compose.selfhost.yml up -d --build
```

Migrations run automatically on backend startup. They are idempotent — running them multiple times has no effect.
