# Deploying Multica on Railway

This guide walks you through deploying the full Multica stack (PostgreSQL, backend API, frontend) on [Railway](https://railway.app).

## Architecture on Railway

| Service | Dockerfile | Internal URL |
|---------|-----------|--------------|
| `postgres` | Railway PostgreSQL plugin | `${{Postgres.DATABASE_URL}}` |
| `backend` | `Dockerfile` | `http://backend.railway.internal:8080` |
| `frontend` | `Dockerfile.web` | Public Railway domain / custom domain |

The frontend proxies all API, WebSocket, auth, and upload requests to the backend through Next.js rewrites — the browser never talks to the backend directly.

## Prerequisites

- [Railway account](https://railway.app)
- Your fork of this repository pushed to GitHub
- Railway CLI (optional): `npm i -g @railway/cli`

---

## Step 1 — Create a Railway Project

1. Go to [railway.app/new](https://railway.app/new) and click **Start a New Project**.
2. Choose **Empty Project** and give it a name (e.g. `multica`).

---

## Step 2 — Add PostgreSQL

1. In your project dashboard, click **+ New** → **Database** → **Add PostgreSQL**.
2. Railway automatically provisions a `pgvector`-compatible PostgreSQL instance and exposes `DATABASE_URL`.

> **Note:** Multica requires the `pgvector` extension. Railway's PostgreSQL plugin includes it. If you use an external database, ensure `pgvector` is installed on PostgreSQL 17+.

---

## Step 3 — Deploy the Backend

1. Click **+ New** → **GitHub Repo** → select your fork.
2. Railway auto-detects `railway.toml` at the repository root and will use `Dockerfile` as the builder.
3. **Before the first deploy**, set the following environment variables in **Service → Variables**:

### Required backend variables

| Variable | Value / Notes |
|----------|---------------|
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` (Railway reference — copy as-is) |
| `JWT_SECRET` | Generate: `openssl rand -hex 32` |
| `APP_ENV` | `production` |
| `PORT` | `8080` (keep fixed so the frontend can reach it internally) |
| `FRONTEND_ORIGIN` | Frontend public URL, e.g. `https://multica-web.up.railway.app` (set after Step 4) |
| `MULTICA_APP_URL` | Same as `FRONTEND_ORIGIN` |
| `MULTICA_PUBLIC_URL` | Backend public URL, e.g. `https://multica-backend.up.railway.app` |

### Optional backend variables

| Variable | Notes |
|----------|-------|
| `RESEND_API_KEY` | Email delivery (recommended for production) |
| `RESEND_FROM_EMAIL` | Sender address, default `noreply@multica.ai` |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` | Alternative to Resend |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth login |
| `GOOGLE_REDIRECT_URI` | e.g. `https://multica-web.up.railway.app/auth/callback` |
| `S3_BUCKET` / `S3_REGION` | File uploads via S3 (leave unset to use local disk storage) |
| `CLOUDFRONT_DOMAIN` / `CLOUDFRONT_KEY_PAIR_ID` / `CLOUDFRONT_PRIVATE_KEY` | CloudFront CDN for uploads |
| `CORS_ALLOWED_ORIGINS` | Comma-separated allowed origins if needed |
| `ALLOW_SIGNUP` | `true` (default) — set `false` to lock registration |
| `ALLOWED_EMAIL_DOMAINS` | Restrict signups to specific domains |
| `MULTICA_DEV_VERIFICATION_CODE` | **Never set in production** — development bypass only |
| `REDIS_URL` | Redis for rate limiting (optional) |
| `GITHUB_APP_SLUG` / `GITHUB_WEBHOOK_SECRET` | GitHub App integration |

4. Click **Deploy**. The entrypoint automatically runs database migrations before starting the server.
5. Health check: Railway polls `GET /live` and waits up to 300 s for a `200 OK`.

---

## Step 4 — Deploy the Frontend

Because `Dockerfile.web` requires the full monorepo as its Docker build context, the frontend service must also use `/` (repository root) as its root directory, with the Dockerfile path overridden in Railway settings.

1. Click **+ New** → **GitHub Repo** → select the **same fork**.
2. Railway will again detect `railway.toml` (backend config). Override it:
   - Go to **Service → Settings → Build**
   - Set **Dockerfile Path** → `Dockerfile.web`
   - Set **Build Context** → `/` (repository root — ensures pnpm workspaces are available)
3. Set the following environment variables in **Service → Variables**:

### Required frontend variables

| Variable | Value / Notes |
|----------|---------------|
| `REMOTE_API_URL` | `http://backend.railway.internal:8080` (Railway private network — replace `backend` with your backend service name if different) |
| `NEXT_PUBLIC_WS_URL` | Backend **public** WebSocket URL, e.g. `wss://multica-backend.up.railway.app` |

### Optional frontend variables

| Variable | Notes |
|----------|-------|
| `NEXT_PUBLIC_APP_VERSION` | App version string shown in UI, defaults to `dev` |
| `POSTHOG_API_KEY` / `POSTHOG_HOST` | Product analytics |

4. Click **Deploy**. The Next.js standalone server starts on `$PORT` (Railway-injected).

---

## Step 5 — Wire Up Cross-Service URLs

After both services are deployed and have public URLs:

1. **Backend** → Variables → set `FRONTEND_ORIGIN` and `MULTICA_APP_URL` to the frontend's Railway domain.
2. **Frontend** → Variables → confirm `REMOTE_API_URL` matches the backend's private URL and `NEXT_PUBLIC_WS_URL` matches the backend's public domain.
3. Redeploy both services for the new variables to take effect.

---

## Internal Networking

Railway services in the same project communicate over a private network using the pattern:

```
http://<service-name>.railway.internal:<PORT>
```

The backend service is named `backend` by default and listens on port `8080` (set `PORT=8080` explicitly to keep it stable). The frontend reaches it at:

```
http://backend.railway.internal:8080
```

If you rename the backend service in Railway, update `REMOTE_API_URL` in the frontend accordingly.

---

## Custom Domains

1. In **Service → Settings → Networking**, click **Generate Domain** to get a Railway subdomain, or **Add Custom Domain** to use your own.
2. Update `FRONTEND_ORIGIN`, `MULTICA_APP_URL`, and `MULTICA_PUBLIC_URL` in the backend to match the production URLs.
3. Update `NEXT_PUBLIC_WS_URL` in the frontend to the backend's final domain.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Backend exits immediately | Check `DATABASE_URL` is set and the PostgreSQL plugin is running |
| `migrate: connection refused` | Railway deploys services in parallel — the PostgreSQL healthcheck must pass before the backend starts; Railway retries automatically |
| Frontend shows blank page / API errors | Verify `REMOTE_API_URL` points to the correct backend private URL |
| WebSocket disconnects | Ensure `NEXT_PUBLIC_WS_URL` uses `wss://` (not `ws://`) for production |
| Email verification not arriving | Set `RESEND_API_KEY` or SMTP variables; in dev set `MULTICA_DEV_VERIFICATION_CODE=888888` (never in production) |
| Build fails: `pnpm install --frozen-lockfile` | The lockfile is checked in — ensure no local changes to `pnpm-lock.yaml` were accidentally committed |

---

## One-Click Deploy (optional)

Add this button to your README after forking:

```markdown
[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/template/new?template=https://github.com/<your-org>/multica)
```

Replace `<your-org>` with your GitHub username or organization.
