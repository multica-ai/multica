# Self-Hosting — Advanced Configuration

This document covers advanced configuration for self-hosted Multica deployments. For the quick start guide, see [SELF_HOSTING.md](SELF_HOSTING.md).

## Configuration

All configuration is done via environment variables. Copy `.env.example` as a starting point.

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://multica:multica@localhost:5432/multica?sslmode=disable` |
| `JWT_SECRET` | **Required — no safe default.** Secret key for signing JWT tokens. A production backend refuses to boot if this is empty or a known placeholder. Generate with `openssl rand -hex 32`. | `openssl rand -hex 32` |
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

Uploads and attachments are written to local disk by default. Set `S3_BUCKET` to
use S3-compatible object storage instead.

#### Local disk (default)

| Variable | Description |
|----------|-------------|
| `LOCAL_UPLOAD_DIR` | Directory attachments are written to (default: `./data/uploads`). The default is **relative to the backend's working directory** — `/app` in the bundled image, where the compose file mounts the `backend_uploads` volume. When running the binary manually it resolves against whatever directory you launched from, so set an absolute path; otherwise uploads land somewhere new each time the launch directory changes, and existing `attachment` rows point at files the server no longer looks for |
| `LOCAL_UPLOAD_BASE_URL` | Optional absolute base for attachment URLs (e.g. `http://localhost:8080`). Leave empty to store `/uploads/...` paths that stay relative to the API origin |

#### S3 / CloudFront

| Variable | Description |
|----------|-------------|
| `S3_BUCKET` | Bucket name only (e.g. `my-bucket`). Do **not** include the `.s3.<region>.amazonaws.com` suffix — the server constructs the public URL from `S3_BUCKET` + `S3_REGION` |
| `S3_REGION` | AWS region (default: `us-west-2`). Must match the bucket's actual region — used for both SDK signing and public URLs |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Static credentials. When both are unset, the AWS SDK default credential chain is used |
| `AWS_ENDPOINT_URL` | Custom S3-compatible endpoint (e.g. MinIO, R2, B2). Setting this defaults to path-style URLs for backward compatibility |
| `S3_USE_PATH_STYLE` | Optional S3 addressing mode. Leave empty for the default (`true` when `AWS_ENDPOINT_URL` is set, `false` for AWS S3). Set `false` for S3-compatible providers that require virtual-hosted-style URLs |
| `ATTACHMENT_DOWNLOAD_MODE` | Attachment download behavior: `auto` (default), `cloudfront`, `presign`, or `proxy`. Use `proxy` for private buckets behind Docker/VPC-only endpoints such as `http://rustfs:9000`. Avatars follow the same policy — see below |
| `ATTACHMENT_DOWNLOAD_URL_TTL` | TTL for CloudFront signed URLs and S3 presigned download URLs (default: `30m`) |
| `CLOUDFRONT_DOMAIN` | CloudFront distribution domain — when set, public URLs use this host instead of the S3 host |
| `CLOUDFRONT_KEY_PAIR_ID` | CloudFront key pair ID for signed URLs |
| `CLOUDFRONT_PRIVATE_KEY` | CloudFront private key (PEM format) |

#### Avatars on a private bucket

User / agent / squad / workspace avatars are stored as the raw storage object
URL. When the bucket is public — a public `CLOUDFRONT_DOMAIN`, or the default
local-disk backend — that URL is served to clients unchanged.

When the bucket is private and no public CDN domain is configured (S3 with
Block Public Access, R2, MinIO), the API instead serves avatars from
`/api/avatars/<signature>/<key>` and resolves each request through
`ATTACHMENT_DOWNLOAD_MODE` — a presigned redirect, a CloudFront-signed
redirect, or a proxied body. The signature in the path is what authorizes the
read: an auth-gated URL cannot be used as an `<img src>` from the Desktop app
or a split-origin frontend, because the session cookie is `SameSite=Strict`.

Only image objects resolve through this route, and only ones that are
*avatar-class*: a standalone upload not attached to an issue, comment, chat, or
task. Pointing an `avatar_url` at a file someone attached to an issue is
rejected when it is set and 404s if it was already stored, so a private image
cannot be turned into a public link by way of the avatar field.

No configuration is required, and avatars uploaded before this behavior
existed are fixed without a backfill.

### Cookies

| Variable | Description |
|----------|-------------|
| `COOKIE_DOMAIN` | Optional `Domain` attribute for session + CloudFront cookies. **Leave empty** for single-host deployments (localhost, LAN IP, or a single hostname). Only set it when the frontend and backend sit on different subdomains of one registered domain (e.g. `.example.com`). **Do not use an IP literal** — RFC 6265 forbids IP addresses in the cookie `Domain` attribute and browsers will drop such `Set-Cookie` headers. |

The `Secure` flag on session cookies is derived automatically from the scheme of `FRONTEND_ORIGIN`: HTTPS origins get `Secure` cookies; plain-HTTP origins (LAN / private-network self-host) get non-secure cookies so the browser can actually store them.

If the frontend and backend are served from different hostnames, `COOKIE_DOMAIN` is **required**, not optional: the browser must be able to read the `multica_csrf` cookie from the page's own origin to send the `X-CSRF-Token` header, and without it every write request fails with `403 {"error":"CSRF validation failed"}`. Scope it to the narrowest parent domain that covers both hosts — it also shares the `multica_auth` session cookie with every host under that domain. See [Reverse Proxy](#reverse-proxy) for the full split-domain configuration and its trust requirements.

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend port — the one to edit. It is the port the backend process listens on for a local/bare run, and the host port the Compose self-host stack publishes. In Compose the container always listens on `8080` internally, so changing this needs no rebuild. |
| `BACKEND_PORT` | Value of `PORT` | Optional alias that overrides `PORT` for the backend. `API_PORT` and `SERVER_PORT` are further aliases; the **alias order** is `BACKEND_PORT` → `API_PORT` → `SERVER_PORT` → `PORT` → `8080`, and it is the same in `Makefile`, `scripts/local-env.sh` and `docker-compose.selfhost.yml`. Leave them unset unless the host port must differ from the port the process listens on. |
| `METRICS_ADDR` | empty | Optional Prometheus metrics listener, for example `127.0.0.1:9090` |
| `FRONTEND_PORT` | `3000` | Frontend port. Host port in Compose; the container always listens on `3000` internally. |
| `CORS_ALLOWED_ORIGINS` | Value of `FRONTEND_ORIGIN` | Comma-separated list of allowed origins. Governs **both** the HTTP CORS allowlist **and** the WebSocket `Origin` check. A browser origin that isn't listed here (and isn't `localhost`) has its real-time WebSocket upgrade rejected with `403`, so live updates stop working until a manual refresh. |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

> **Which source wins depends on the entry point**, and only the alias order above
> is shared. Docker Compose lets the calling environment outrank `.env`
> (`PORT=9100 docker compose … up -d` publishes 9100). `make` is the other way
> round: it `include`s the env file, so a value there outranks the same variable
> from your environment, and a `make selfhost PORT=…` command-line assignment
> outranks both. Editing the env file is therefore the one method that behaves
> the same everywhere. None of this affects whether the reported port is correct:
> `make selfhost` and both installers read the published port back from
> `docker compose port`, so the health check and the printed URL always match
> what Compose actually published.

The web development server is one intentional exception to the generic `PORT`
fallback: Next uses `PORT` for its own frontend listener before it evaluates the
rewrite configuration. Its backend fallback therefore accepts
`BACKEND_PORT` → `API_PORT` → `SERVER_PORT` → `8080`, while an explicit
`REMOTE_API_URL` or `NEXT_PUBLIC_API_URL` still takes priority.

### WeCom frame tracing

| Variable | Default | Description |
|----------|---------|-------------|
| `MULTICA_WECOM_TRACE` | empty (off) | `1` records every WeCom frame the backend reads and writes, including the first 120 runes of each message body. Anything else is off. |

Turn it on for a debugging session and unset it when the session ends. Before
you do:

- **It is read at boot, so changing it needs a backend restart** — `docker compose -f docker-compose.selfhost.yml up -d backend`. There is no runtime toggle. While it is on, the backend logs a warning on every startup saying so.
- **Anyone who can read the backend's logs can read the traced message text.** That is a wider audience than the WeCom chat it came from: your `docker logs` / journald / log shipper, and whoever administers them. The smart-bot secret is never read, and `token=` / `access_token=` / `code=` parameters are redacted out of message text — but user message content is not, and **an attachment's `Content-Disposition` and the filename read out of it are written verbatim**, up to 2048 runes and past the redactor. That line exists to show exactly how the storage backend encoded a name, and a redacted or truncated copy of it answers nothing; it also means an attachment's filename reaches your logs as sent.
- **Retention is your log stack's, not the application's.** The backend writes to stderr and keeps nothing itself, so how long the traced text survives is whatever your Docker logging driver or shipper is set to. If that is "forever", decide about it before enabling rather than after.

Each outbound frame produces two lines that share a `seq`: `dir=out` when it is
about to be written, and `dir=out.done` with `ok=true` / `ok=false` once the
socket has answered. `seq` is the frame's position in the write order, so the
pair tells you what this backend sent and in which order; an attempt with no
matching outcome is a write that never returned.

`ok=true` means the frame reached the socket, not that WeCom accepted it. For
the platform's verdict, match the frame's `req_id` against the `dir=in` line
answering it and read that line's `errcode` — a frame can be written
successfully and still be rejected there.

### CLI / Daemon

These are configured on each user's machine, not on the server:

| Variable | Default | Description |
|----------|---------|-------------|
| `MULTICA_SERVER_URL` | `ws://localhost:8080/ws` | WebSocket URL for daemon → server connection |
| `MULTICA_APP_URL` | `http://localhost:3000` | Frontend URL for CLI login flow |
| `MULTICA_DAEMON_POLL_INTERVAL` | `30s` | Catch-up poll for tasks; WebSocket wake signals normally deliver work sooner |
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
| `MULTICA_CODEARTS_PATH` | Custom path to the `codearts` launcher or binary |
| `MULTICA_CODEARTS_MODEL` | Override the CodeArts model used |
| `MULTICA_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `MULTICA_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `MULTICA_OPENCLAW_CLI_TIMEOUT` | Deadline for each `openclaw config ...` call during task preparation (default 30s; accepts `45s` or `45`). Raise it when the local CLI is slow to start; the daemon also reads it from `backends.openclaw.cli_timeout` in the CLI config |
| `MULTICA_HERMES_PATH` | Custom path to the `hermes` binary |
| `MULTICA_HERMES_MODEL` | Override the Hermes model used |
| `MULTICA_PI_PATH` | Custom path to the `pi` binary |
| `MULTICA_PI_MODEL` | Override the Pi model used |
| `MULTICA_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `MULTICA_CURSOR_MODEL` | Override the Cursor Agent model used |
| `MULTICA_GROK_PATH` | Custom path to the `grok` binary |
| `MULTICA_GROK_MODEL` | Override the Grok model used (e.g. `grok-4.5`) |

## Database Setup

Multica requires PostgreSQL 17. It does **not** use pgvector — no migration
declares a `vector` column or runs `CREATE EXTENSION vector`. The bundled image
is named `pgvector/pgvector:pg17` for historical reasons only; a stock
PostgreSQL 17 is sufficient.

Migrations reference four extensions:

| Extension | Migration | Required |
| --- | --- | --- |
| `pgcrypto` | `001_init` | **Yes** — a bare `CREATE EXTENSION`; the migration fails without it |
| `pg_trgm` | `137_search_index_pg_trgm_extension` | **Yes** — same, and the search indexes depend on it |
| `pg_bigm` | `032_issue_search_index` | No — wrapped in `DO ... EXCEPTION`, skipped with a `NOTICE` when unavailable |
| `pg_cron` | `076_task_usage_pgcron_extension` | No — same; the usage rollup runs in-process instead, see [Usage Dashboard Rollup](#usage-dashboard-rollup) |

`pgcrypto` and `pg_trgm` ship with every standard PostgreSQL 17 build
(`postgresql-contrib` on Debian/Ubuntu, bundled in Homebrew's `postgresql@17`
and in the official `postgres:17` image), so neither hard requirement needs a
custom image.

`pg_bigm` is optional and only affects **CJK search quality** — bigram indexes
are far more selective than trigram ones for Chinese, Japanese, and Korean text.
Search works without it: migrations 138–142 install portable `pg_trgm` fallback
indexes covering every column the search handlers touch. Installing `pg_bigm`
does require a custom image or a source build — the bundled
`pgvector/pgvector:pg17` image does not ship it either.

> Because `pg_bigm` and `pg_cron` are installed conditionally, a row in
> `schema_migrations` proves the migration ran, not that its objects exist. Run
> `SELECT extname FROM pg_extension` to see what a database actually has. To
> recover a missing comment-content search index, follow
> `server/cmd/migrate/README.md`.

### Using Docker Compose (Recommended)

The `docker-compose.selfhost.yml` includes PostgreSQL. No separate setup needed.

### Using Your Own PostgreSQL

If you prefer to use an existing PostgreSQL instance, confirm the migration role
can create the two required extensions:

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS pg_trgm;
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

**Prerequisites:** Go 1.26.6, Node.js 22, pnpm 10.28.2, PostgreSQL 17 — a stock install is enough, see [Database Setup](#database-setup).

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
COOKIE_DOMAIN=.example.com           # narrowest parent covering both hosts — read the scope warning below

# Frontend (only if you are building the web image from source via docker-compose.selfhost.build.yml)
REMOTE_API_URL=https://api.example.com
NEXT_PUBLIC_API_URL=https://api.example.com
NEXT_PUBLIC_WS_URL=wss://api.example.com/ws
```

> **`NEXT_PUBLIC_API_URL` and `REMOTE_API_URL` take the backend's origin — scheme + host (+ port) — and no path.** Write `https://api.example.com`, never `https://api.example.com/api`. The browser client appends its own prefixes, so a path is doubled: requests go to `/api/api/...` and every avatar, image and attachment resolves under `<your-path>/uploads/...`, which the backend does not serve — the app loads but data calls and images 404. A trailing `/api` is now stripped defensively, but any other path is kept, because a reverse proxy may legitimately mount the whole backend under a prefix.
>
> This bites on upgrade: before v0.4.10 `NEXT_PUBLIC_API_URL` was inert in the published images, so a wrong value sat in `.env` doing nothing. v0.4.10 wired it through, and the stale value took effect. If you upgraded and the UI loads but nothing else does, check this variable first, then recreate the frontend container (`docker compose ... up -d --force-recreate frontend`) so the new value is picked up.

> **`COOKIE_DOMAIN` is required in this setup — omitting it breaks every write.** The web app authenticates with an HttpOnly `multica_auth` cookie plus a JS-readable `multica_csrf` cookie, and sends the CSRF value as an `X-CSRF-Token` header on every non-GET request. Both cookies are host-only unless `COOKIE_DOMAIN` is set, so a frontend on `app.example.com` cannot read a cookie issued by `api.example.com`. The header is then never sent and the backend rejects the request with `403 {"error":"CSRF validation failed"}` — while GET requests keep working, so the app renders but nothing can be created or edited.
>
> After changing `COOKIE_DOMAIN`, delete the existing `multica_auth` / `multica_csrf` cookies on **both** hosts and log in again. Stale host-only cookies otherwise sit alongside the new domain-scoped ones and the browser sends both.

> **Scope `COOKIE_DOMAIN` as narrowly as possible.** The same value also scopes `multica_auth`, the session JWT, and the browser then sends it to **every** host under that domain. `HttpOnly` only stops page scripts from reading the cookie — it does not stop the server behind a sibling subdomain from receiving it on every request. Use the narrowest parent that still covers both hosts: for `agent.example.com` + `api.agent.example.com` that is `.agent.example.com`, **not** `.example.com`, which would also hand your users' session cookie to unrelated hosts such as a separately deployed `docs.example.com`.

Both of these must hold, or this layout is not safe to use:

- The frontend and backend share a common parent domain. Unrelated domains (`app.com` and `api.io`) cannot be covered by any `COOKIE_DOMAIN` value.
- Every host under that parent domain is operated by the same trusted party. If anything in scope is third-party hosted, or other teams can create records under it, a compromise or a rogue service there receives valid sessions for your deployment.

If either condition fails, use the same-origin layout below, which keeps the session cookie host-only.

### Same Origin (Recommended)

A separate API domain is not required. Leave `NEXT_PUBLIC_API_URL` and `NEXT_PUBLIC_WS_URL` at their defaults and the browser calls `/api` and `/ws` on the page's own origin, so no cross-host cookie problem can arise in the first place:

```bash
# Backend
FRONTEND_ORIGIN=https://app.example.com
CORS_ALLOWED_ORIGINS=https://app.example.com
COOKIE_DOMAIN=                       # empty: cookies are host-only on app.example.com, which is correct here

# Frontend
NEXT_PUBLIC_API_URL=                 # empty: the client uses relative /api paths on the page origin
NEXT_PUBLIC_WS_URL=                  # empty
REMOTE_API_URL=http://backend:8080   # target the Next.js rewrites proxy /v1, /api, /auth and /uploads to
```

Serve everything from the single `app.example.com` vhost. HTTP works out of the box, because Next.js rewrites forward `/v1`, `/api`, `/auth` and `/uploads` to `REMOTE_API_URL`. WebSockets do **not** go through those rewrites, so add a `/ws` block to the frontend vhost that reaches the backend directly:

```nginx
# Add to the app.example.com server block above
location /ws {
    proxy_pass http://localhost:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 86400;
}
```

See [WebSocket for LAN / Non-localhost Access](#websocket-for-lan--non-localhost-access) for why the rewrites cannot carry the `Upgrade` handshake.

This keeps cookies, CORS, and the WebSocket origin check on a single origin. It is both the configuration least likely to break and the safer one: the session cookie stays host-only, so no sibling subdomain can ever receive it. An `api.example.com` vhost can still be kept for CLI and daemon use: those clients authenticate with a `mul_` personal access token over `Authorization: Bearer`, which never goes through the cookie or CSRF path.

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

HTTP requests (Plugin API, issues, comments, uploads) work on LAN out of the box — Next.js rewrites proxy `/v1`, `/api`, `/auth`, and `/uploads` to the backend. **WebSockets do not**: Next.js rewrites only forward HTTP requests, not the `Upgrade` handshake a WebSocket needs. If you open the app on `http://<lan-ip>:3000`, real-time features (chat streaming, live issue updates, notifications) will fail to connect until you do one of the following:

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

## Go Runtime Profiling

The backend exposes all standard Go pprof routes on the fixed loopback-only
management listener `127.0.0.1:6060`, including CPU, heap, allocs, goroutine,
block, mutex, threadcreate, symbol, and trace profiles:

```bash
go tool pprof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

The public API port does not serve `/debug/pprof/`. The listener address is not
configurable and is never bound to a container or host network interface.
Profiles can reveal process internals and some captures add CPU or memory
pressure, so access should remain limited to operators on the same host or in
the same container network namespace.

A loopback listener inside a container belongs to that container's network
namespace and is not reachable directly from the host. With the Compose stack,
capture the profile inside the backend container and copy it out:

```bash
docker compose -f docker-compose.selfhost.yml exec backend \
  wget -qO /tmp/heap.pprof http://127.0.0.1:6060/debug/pprof/heap
docker compose -f docker-compose.selfhost.yml cp backend:/tmp/heap.pprof ./heap.pprof
go tool pprof ./heap.pprof
```

## Upgrading

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

Pin `MULTICA_IMAGE_TAG` in `.env` to an exact release like `v0.2.4` if you want to stay on a specific version. Migrations run automatically on backend startup. They are idempotent — running them multiple times has no effect.
If the selected GHCR tag has not been published yet, fall back to `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build`.
