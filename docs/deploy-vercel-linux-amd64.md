# Vercel Frontend + Linux AMD64 Backend

This guide matches the current repository layout:

- `apps/workspace` is the user-facing SPA
- `server` is the Go backend
- `apps/web` is the marketing site and is not required for the app deployment below

## 1. Deploy the frontend to Vercel

Create a new Vercel project from this repository.

Use these project settings:

- Root Directory: `apps/workspace`
- Framework Preset: `Vite`
- Install Command: keep the default Vercel value
- Build Command: keep the default Vercel value, or set `pnpm build`
- Output Directory: `dist`

The repository now includes [apps/workspace/vercel.json](../apps/workspace/vercel.json), which enables SPA deep-link fallback to `index.html`.

Set these environment variables in Vercel:

- `VITE_API_URL=https://api.example.com`
- `VITE_WS_URL=wss://api.example.com/ws`

After the first deployment, note the final frontend URL. If you later bind a custom domain, use that custom domain in the backend configuration instead of the temporary `.vercel.app` domain.

## 2. Build the backend for linux/amd64

Run this from the repository root on your development machine:

```bash
pnpm install
RELEASE_TARGETS=linux/amd64 bash scripts/package-release-multi.sh dist/backend-linux-amd64
```

This produces:

- release directory: `dist/backend-linux-amd64/multica-backend-linux-amd64`
- tarball: `dist/backend-linux-amd64/multica-backend-linux-amd64.tar.gz`
- shared workspace directory: `dist/backend-linux-amd64/workspace`

The package includes:

- `server`
- `migrate`
- `migrations/`
- `config/server.env.example`

The shared `workspace/` directory is generated once by the multi-platform packaging flow. For this Vercel deployment path, you can ignore it because the frontend is already deployed separately.

## 3. Upload and run the backend on your Linux server

Copy the tarball to your Linux AMD64 host, then run:

```bash
mkdir -p /opt/multica
cd /opt/multica
tar -xzf multica-backend-linux-amd64.tar.gz
cd multica-backend-linux-amd64
cp config/server.env.example .env
```

Edit `.env` and set at least these values:

- `DATABASE_URL`
- `JWT_SECRET`
- `FRONTEND_ORIGIN=https://your-frontend-domain`
- `CORS_ALLOWED_ORIGINS=https://your-frontend-domain`
- `MULTICA_APP_URL=https://your-frontend-domain`
- `GOOGLE_REDIRECT_URI=https://your-frontend-domain/auth/callback`
- `RESEND_API_KEY`
- `RESEND_FROM_EMAIL`

Then run migrations and start the server:

```bash
set -a
. ./.env
set +a

./migrate up
./server
```

By default the backend listens on `:8080`.

## 4. Put the backend behind HTTPS

Expose the Go server through a reverse proxy such as Nginx or Caddy, and give it a public domain such as `api.example.com`.

Your frontend variables should then be:

- `VITE_API_URL=https://api.example.com`
- `VITE_WS_URL=wss://api.example.com/ws`

Your backend variables should then be:

- `FRONTEND_ORIGIN=https://app.example.com`
- `CORS_ALLOWED_ORIGINS=https://app.example.com`
- `MULTICA_APP_URL=https://app.example.com`
- `GOOGLE_REDIRECT_URI=https://app.example.com/auth/callback`

## 5. Notes and constraints

- This deployment path only covers the workspace SPA in `apps/workspace`.
- If you also want the marketing site, deploy `apps/web` as a separate Vercel project.
- The backend returns JWTs in JSON, so split-domain login works.
- If you enable CloudFront signed cookies for private file delivery, keep `COOKIE_DOMAIN` and `CLOUDFRONT_DOMAIN` aligned with your final public domains.