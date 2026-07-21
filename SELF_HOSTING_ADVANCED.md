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

Multica supports two email backends. `SMTP_HOST` takes priority when set; otherwise `RESEND_API_KEY` is used. With neither configured, verification codes are printed to the server log — copy them from there to log in.

#### Option A: Resend (recommended for cloud deployments)

| Variable | Description |
|----------|-------------|
| `RESEND_API_KEY` | Your Resend API key |
| `RESEND_FROM_EMAIL` | Sender email address (default: `noreply@multica.ai`) |

#### Option B: SMTP relay (for self-hosted / on-premise deployments)

Use this option when your deployment cannot reach the public internet or you already have an internal mail relay (e.g. Exchange, Postfix, SendGrid on-prem).

| Variable | Description | Default |
|----------|-------------|----------|
| `SMTP_HOST` | SMTP relay hostname (setting this activates SMTP mode) | - |
| `SMTP_PORT` | SMTP port | `25` |
| `SMTP_USERNAME` | SMTP username (leave empty for unauthenticated relay) | - |
| `SMTP_PASSWORD` | SMTP password | - |
| `SMTP_TLS` | TLS mode. `implicit` (aliases `smtps`, `ssl`) forces SMTPS on connect; port `465` auto-enables it. Unset / `starttls` upgrades via STARTTLS | `starttls` |
| `SMTP_TLS_INSECURE` | Set `true` to skip TLS certificate verification (self-signed / private CA certs) | `false` |
| `SMTP_EHLO_NAME` | EHLO/HELO name announced to the relay. Set a real FQDN when a strict relay (e.g. Google Workspace) rejects the default greeting from a public IP | machine hostname |

STARTTLS is used automatically when advertised by the server. Port 465 (SMTPS / implicit TLS) is supported and auto-enables implicit TLS; set `SMTP_TLS=implicit` (aliases `smtps`, `ssl`) to force it on a non-standard port.

> **Note:** If neither Resend nor SMTP is configured, generated verification codes are printed to backend logs — copy them from there to log in. A fixed local testing code (e.g. `888888`) is **opt-in only**: set `MULTICA_DEV_VERIFICATION_CODE=888888` in `.env` and keep `APP_ENV` non-production. The Docker self-host stack pins `APP_ENV=production`, so the shortcut is ignored there. **Never enable a fixed code on a publicly reachable instance.**

### Google OAuth (Optional)

| Variable | Description |
|----------|-------------|
| `GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret |
| `GOOGLE_REDIRECT_URI` | OAuth callback URL (e.g. `https://app.example.com/auth/callback`) |

Changes take effect after restarting the backend / compose stack. The web UI reads `GOOGLE_CLIENT_ID` from `/api/config` at runtime, so no web rebuild is needed.

### Signup Controls (Optional)

| Variable | Description |
|----------|-------------|
| `ALLOW_SIGNUP` | Set to `false` to disable new user signups on a private instance |
| `ALLOWED_EMAIL_DOMAINS` | Optional comma-separated allowlist of email domains |
| `ALLOWED_EMAILS` | Optional comma-separated allowlist of exact email addresses |
| `DISABLE_WORKSPACE_CREATION` | Set to `true` to make `POST /api/workspaces` return 403 for every caller — users can only join workspaces they were invited to |

Changes take effect after restarting the backend / compose stack. The web UI reads `ALLOW_SIGNUP` and `DISABLE_WORKSPACE_CREATION` from `/api/config` at runtime, so no web rebuild is needed.

#### Locking down workspace creation

`ALLOW_SIGNUP=false` blocks new accounts from being created, but it does **not** block an already-signed-in user from creating another workspace via `POST /api/workspaces`. On a self-hosted instance where every issue/repo/agent must be visible to the platform admin, set `DISABLE_WORKSPACE_CREATION=true` to close that gap. The recommended bootstrap sequence is:

1. Start the instance with `DISABLE_WORKSPACE_CREATION=false` (the default).
2. Sign in as the admin and create the shared workspace.
3. Set `DISABLE_WORKSPACE_CREATION=true` and restart the backend. Optionally set `ALLOW_SIGNUP=false` at the same time if you also want to block new account creation.
4. Going forward, additional users join via invitation only — the "Create workspace" affordance is hidden in the UI and any direct API call returns 403.

> Note: setting `ALLOW_SIGNUP=false` blocks **all** new account creation, including users who already have a pending invitation. If you need invited users to be able to sign up but not create their own workspaces, keep `ALLOW_SIGNUP=true` (optionally combined with `ALLOWED_EMAIL_DOMAINS` / `ALLOWED_EMAILS`) and only flip `DISABLE_WORKSPACE_CREATION=true`.

### File Storage (Optional)

For file uploads and attachments, configure S3 and (optionally) CloudFront:

| Variable | Description |
|----------|-------------|
| `S3_BUCKET` | Bucket name only (e.g. `my-bucket`). Do **not** include the `.s3.<region>.amazonaws.com` suffix — the server constructs the public URL from `S3_BUCKET` + `S3_REGION` |
| `S3_REGION` | AWS region (default: `us-west-2`). Must match the bucket's actual region — used for both SDK signing and public URLs |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Static credentials. When both are unset, the AWS SDK default credential chain is used |
| `AWS_ENDPOINT_URL` | Custom S3-compatible endpoint (e.g. MinIO, R2, B2). Setting this defaults to path-style URLs for backward compatibility |
| `S3_USE_PATH_STYLE` | Optional S3 addressing mode. Leave empty for the default (`true` when `AWS_ENDPOINT_URL` is set, `false` for AWS S3). Set `false` for S3-compatible providers that require virtual-hosted-style URLs |
| `ATTACHMENT_DOWNLOAD_MODE` | Attachment download behavior: `auto` (default), `cloudfront`, `presign`, or `proxy`. Use `proxy` for private buckets behind Docker/VPC-only endpoints such as `http://rustfs:9000` |
| `ATTACHMENT_DOWNLOAD_URL_TTL` | TTL for CloudFront signed URLs and S3 presigned download URLs (default: `30m`) |
| `CLOUDFRONT_DOMAIN` | CloudFront distribution domain — when set, public URLs use this host instead of the S3 host |
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
| `METRICS_ADDR` | empty | Optional Prometheus metrics listener, for example `127.0.0.1:9090` |
| `FRONTEND_PORT` | `3000` | Frontend port |
| `CORS_ALLOWED_ORIGINS` | Value of `FRONTEND_ORIGIN` | Comma-separated list of allowed origins. Governs **both** the HTTP CORS allowlist **and** the WebSocket `Origin` check. A browser origin that isn't listed here (and isn't `localhost`) has its real-time WebSocket upgrade rejected with `403`, so live updates stop working until a manual refresh. |
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
| `MULTICA_COPILOT_PATH` | Custom path to the `copilot` (GitHub Copilot CLI) binary |
| `MULTICA_COPILOT_MODEL` | Override the Copilot model used (note: GitHub Copilot routes models through your account entitlement, so this may not be honoured) |
| `MULTICA_OPENCODE_PATH` | Custom path to the `opencode` binary |
| `MULTICA_OPENCODE_MODEL` | Override the OpenCode model used |
| `MULTICA_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `MULTICA_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `MULTICA_HERMES_PATH` | Custom path to the `hermes` binary |
| `MULTICA_HERMES_MODEL` | Override the Hermes model used |
| `MULTICA_PI_PATH` | Custom path to the `pi` binary |
| `MULTICA_PI_MODEL` | Override the Pi model used |
| `MULTICA_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `MULTICA_CURSOR_MODEL` | Override the Cursor Agent model used |
| `MULTICA_GROK_PATH` | Custom path to the `grok` binary |
| `MULTICA_GROK_MODEL` | Override the Grok model used (e.g. `grok-4.5`) |

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

## Usage Dashboard Rollup

The Usage and Runtime dashboards read from `task_usage_hourly`, a derived table populated by `rollup_task_usage_hourly()`. As of MUL-2957 the backend runs this rollup **in-process** on every replica via a DB-backed scheduler (`sys_cron_executions`); a fresh self-host install needs no operator action — the bundled `pgvector/pgvector:pg17` image works without changes.

### How the in-process scheduler works

Every backend replica ticks every 30 seconds and tries to claim the current 5-minute UTC plan in `sys_cron_executions`. The unique key `(job_name, scope_kind, scope_id, plan_time)` makes the claim a single-winner contest across all replicas, so multi-instance deployments do not double-write. The handler then calls `SELECT rollup_task_usage_hourly()`; the SQL function holds advisory lock `4246` internally, so a stray `pg_cron` job or manual call can run alongside the scheduler without ever colliding on the rollup itself. Inspect the audit table for steady-state operation:

```sql
SELECT plan_time, status, attempt, runner_id,
       error_code, error_msg, started_at, finished_at
  FROM sys_cron_executions
 WHERE job_name = 'rollup_task_usage_hourly'
 ORDER BY plan_time DESC
 LIMIT 20;
```

### Compatibility — existing `pg_cron` registrations

If you previously registered the rollup as a `pg_cron` job (`SELECT cron.schedule('rollup_task_usage_hourly', '*/5 * * * *', …)`), it is safe to leave it in place: advisory lock 4246 prevents double-writes, and the loser path no-ops cleanly. To drop the redundant entry once the in-process scheduler is up:

```sql
SELECT cron.unschedule('rollup_task_usage_hourly')
  FROM cron.job WHERE jobname = 'rollup_task_usage_hourly';
```

External cron / systemd / Kubernetes `CronJob` setups that call `SELECT rollup_task_usage_hourly()` directly are also still valid — they were the only option before MUL-2957 and remain a supported compatibility path. They are no longer the recommended setup; new deployments should rely on the in-process scheduler.

### Standalone backfill command

`rollup_task_usage_hourly()` only processes new buckets after it starts running. If you already have `task_usage` rows from before the rollup was claimed for the first time — most commonly when upgrading from `v0.3.4` to `v0.3.5+` on a database that already has months of usage — you can run `backfill_task_usage_hourly` to seed historical buckets:

```bash
# Docker Compose
docker compose -f docker-compose.selfhost.yml exec backend \
  ./backfill_task_usage_hourly --sleep-between-slices=2s

# Kubernetes
kubectl -n multica exec deploy/multica-backend -- \
  ./backfill_task_usage_hourly --sleep-between-slices=2s
```

The command walks `task_usage`'s full time range in monthly slices and calls the same idempotent primitive the in-process scheduler uses, so it's safe to re-run, to interrupt with Ctrl-C, and to run concurrently with the scheduler (advisory lock 4246 serialises them). Flags:

| Flag | Description |
|---|---|
| `--sleep-between-slices` | Pause between monthly slices to throttle read pressure on busy databases (e.g. `2s`). Recommended on production DBs with years of history. |
| `--months-back N` | Only backfill the last N months. **Requires `--force-partial`** because the watermark still advances past the skipped older buckets — those are permanently abandoned. |
| `--dry-run` | Log slices that would be processed without writing anything. |

After backfill completes, the rollup-state watermark is stamped to `now() - 5 minutes`, so the first scheduled tick after backfill does not redo history.

### `v0.3.4 → v0.3.5+` upgrade order

Migration `103` adds a fail-closed guard that refuses to drop the legacy daily rollups until `task_usage_hourly` has caught up. As of MUL-2957 the migrate command runs an idempotent monthly-slice backfill (under advisory lock 4246) **automatically** immediately before applying migration `103`, so v0.3.4 → v0.3.5+ upgrades complete in a single `migrate up` invocation — no operator step is required.

If you are upgrading from a binary that pre-dates MUL-2957 (or the auto-hook fails for an environmental reason), recovery is the manual path: run `backfill_task_usage_hourly` against the database, then re-run `migrate up` (or restart the backend container — migrations run automatically on startup). **Fresh installs are exempt** — the guard short-circuits when `task_usage` is empty, and the in-process scheduler picks up new buckets from the first tick.

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

**Single-domain layout** — frontend and backend served on the same hostname (this is what `docker-compose.selfhost.yml` defaults to):

```
multica.example.com {
    # WebSocket route — must come before the catch-all
    @multica_ws path /ws /ws/*
    handle @multica_ws {
        reverse_proxy localhost:8080 {
            flush_interval -1
        }
    }

    # Everything else → frontend
    reverse_proxy localhost:3000
}
```

> Even on a single domain, set `FRONTEND_ORIGIN` / `CORS_ALLOWED_ORIGINS` to that public origin (e.g. `https://multica.example.com`) on the backend. The backend's default origin allowlist is `localhost` only, so without this it rejects the WebSocket upgrade from the public URL with `403` and real-time updates silently stop working. See [LAN / Non-localhost Access](#lan--non-localhost-access).

**Separate-domain layout** — frontend and backend on different hostnames:

```
app.example.com {
    reverse_proxy localhost:3000
}

api.example.com {
    @multica_ws path /ws /ws/*
    handle @multica_ws {
        reverse_proxy localhost:8080 {
            flush_interval -1
        }
    }

    reverse_proxy localhost:8080
}
```

Two non-obvious bits inside the `/ws` block are worth calling out — both are common reasons real-time updates "stop working" on a Caddy-fronted self-host:

- **`path /ws /ws/*` (not `/ws*`)** — bare `handle /ws` is an exact match, so future path variants under `/ws/` fall through to the frontend block. The obvious shortcut `handle /ws*` overcorrects in the other direction: Caddy's `*` is a glob without a path-segment boundary, so it would also catch unrelated paths like `/ws-foo`, which is a legitimate workspace URL (only the exact slug `ws` is reserved). Listing `/ws` and `/ws/*` explicitly covers both real cases without overreach.
- **`flush_interval -1`** — disables response buffering so WebSocket frames are forwarded as soon as they arrive. Without it, frames can sit behind Caddy's default flush window, which looks like delayed comments, missing typing indicators, or "comments only appear after a page refresh."

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

# Frontend (only if you are building the web image from source via docker-compose.selfhost.build.yml)
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

Then restart the stack:

```bash
docker compose -f docker-compose.selfhost.yml up -d
```

### WebSocket for LAN / Non-localhost Access

HTTP requests (issues, comments, uploads) work on LAN out of the box — Next.js rewrites proxy `/api`, `/auth`, and `/uploads` to the backend. **WebSockets do not**: Next.js rewrites only forward HTTP requests, not the `Upgrade` handshake a WebSocket needs. If you open the app on `http://<lan-ip>:3000`, real-time features (chat streaming, live issue updates, notifications) will fail to connect until you do one of the following:

1. **Put a reverse proxy in front of the stack (recommended).** Nginx or Caddy terminates the WebSocket upgrade and forwards it to the backend on port 8080. See the [Reverse Proxy](#reverse-proxy) section above — the Nginx example already includes a `location /ws { ... }` block with the correct `Upgrade` / `Connection` headers. Once a proxy is in place the browser connects directly through it, so no frontend rebuild is needed.

2. **Bake a WebSocket URL into the web image.** If you are not running a reverse proxy, rebuild the web image with `NEXT_PUBLIC_WS_URL` pointing straight at the backend (port 8080 must be reachable from the browser):

   ```bash
   # In .env
   NEXT_PUBLIC_WS_URL=ws://<lan-ip>:8080/ws

   # Rebuild the web image so the build-time value is baked in
   docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
   ```

   `NEXT_PUBLIC_WS_URL` is a build-time variable (see `Dockerfile.web`), so setting it only in `environment:` on the pre-built image has no effect — you must use the `selfhost.build.yml` override that rebuilds the image.

**Also required: allowlist the browser origin.** The two options above fix the WebSocket *upgrade proxying*, but a second, independent setting gates the connection: the backend validates the WebSocket `Origin` header against an allowlist that defaults to `localhost` only. When you open Multica from any other origin — a LAN IP **or a public domain behind a reverse proxy** — set `CORS_ALLOWED_ORIGINS` (or `FRONTEND_ORIGIN`) on the backend to that exact origin and restart, exactly as shown under [LAN / Non-localhost Access](#lan--non-localhost-access) above. Otherwise the upgrade is refused with `403`: the backend logs `websocket: request origin not allowed by Upgrader.CheckOrigin` and the browser console loops `disconnected, reconnecting in 3s`, while HTTP requests (and manual page refreshes) keep working because they are same-origin to the page. The single value covers both HTTP CORS and the WebSocket origin check.

> **Note:** If you need to hard-code a different public API / WebSocket endpoint into the web image for any other reason, use the same source-build override: `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build`.

## Health Check

The backend exposes public health endpoints:

```text
GET /health
→ {"status":"ok"}

GET /readyz
→ {"status":"ok","checks":{"db":"ok","migrations":"ok"}}

GET /healthz
→ same response as /readyz
```

Use `/health` for basic liveness / reachability checks. Use `/readyz` for
dependency-aware readiness probes and external monitoring that should fail when
the database is unavailable or migrations are not fully applied. `/healthz` is
kept as an alias for operator familiarity.

## Prometheus Metrics

The backend can expose Prometheus metrics on a separate management listener:

```bash
METRICS_ADDR=127.0.0.1:9090 ./server/bin/server
curl http://127.0.0.1:9090/metrics
```

`METRICS_ADDR` is empty by default, so no metrics listener is started. The
public API port does not serve `/metrics`; keep it that way for internet-facing
deployments. HTTP request metrics start accumulating only after the metrics
listener is enabled. Metrics can reveal internal routes, traffic volume,
dependency state, and runtime health.

For Docker or Kubernetes deployments, prefer a private scrape path: bind the
metrics listener to an internal interface and protect it with private
networking, allowlists, NetworkPolicy, or proxy authentication. If you bind
`METRICS_ADDR=0.0.0.0:9090` inside a container, only publish that port to a
trusted network, for example a host-local mapping such as
`127.0.0.1:9090:9090`.

## Upgrading

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

Pin `MULTICA_IMAGE_TAG` in `.env` to an exact release like `v0.2.4` if you want to stay on a specific version. Migrations run automatically on backend startup. They are idempotent — running them multiple times has no effect.
If the selected GHCR tag has not been published yet, fall back to `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build`.
