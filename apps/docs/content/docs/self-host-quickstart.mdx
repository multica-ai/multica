---
title: Self-host quickstart
description: Run Multica on your own server or machine with Docker. Takes about 10 minutes.
---

import { Callout } from "fumadocs-ui/components/callout";

This page walks you through running the Multica **server** (backend + frontend + PostgreSQL) on your own machine or server with Docker. When you're done, your data is fully under your control — including [workspaces](/workspaces), [issues](/issues), [comments](/comments), and [agent](/agents) configuration.

Agent **execution** still relies on the [daemon](/daemon-runtimes) you run locally plus the [AI coding tools](/providers) installed on that machine — exactly like Cloud. Self-host swaps out the server layer, not the execution layer.

## Prerequisites

- **Docker** installed and able to run `docker compose`
- **Git** (optional, but recommended so you can pull the source)
- A machine that can stay up (local / internal network / cloud host all work)
- At least one AI coding tool installed on **the machine running the daemon** (not necessarily the one running the server — your dev laptop works)

## 1. Pull the project and start the backend

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
make selfhost
```

`make selfhost` will:

1. Generate a `.env` from `.env.example` if missing, with a **random JWT_SECRET**
2. Pull the official Docker images (PostgreSQL, Multica backend, Multica frontend)
3. Bring up every service using `docker-compose.selfhost.yml`
4. Wait until the backend's `/health` endpoint is ready

For ongoing production probes after startup, use `/readyz` when you want the
check to fail on database or migration problems.

The backend container **runs database migrations automatically** on startup (`docker/entrypoint.sh` runs `./migrate up` before the server starts) — you'll see the migration output in the backend logs. Version upgrades are handled the same way.

<Callout type="info">
**Image not published yet?** If `make selfhost` fails to pull images, you may be on an unreleased version tag. Switch to a stable release, or build from source: `make selfhost-build`.
</Callout>

Once it's up:

- **Frontend**: [http://localhost:3000](http://localhost:3000)
- **Backend**: [http://localhost:8080](http://localhost:8080)

## 2. Important: set `APP_ENV` to `production`

<Callout type="warning">
**`docker-compose.selfhost.yml` sets `APP_ENV` to `production` by default** — this prevents the development "master code `888888`" from being enabled on an instance you've exposed to the public internet.

**But if your `.env` leaves `APP_ENV` empty or sets it to another value**, `888888` is enabled — **anyone can log in as any email by typing `888888` as the verification code**. See [Auth setup → The 888888 trap](/auth-setup#the-888888-trap).

Before any public deployment, make sure `.env` has `APP_ENV=production`.
</Callout>

## 3. Configure the email service (optional but recommended)

Without email configured, your users can't receive verification codes — **unless `APP_ENV != production`, in which case `888888` works** (see the warning above).

To actually send verification emails:

1. Sign up at [Resend](https://resend.com/) and get an API key
2. Verify a sending domain you control
3. Set these in `.env`:

    ```bash
    RESEND_API_KEY=re_xxxxxxxxxxxx
    RESEND_FROM_EMAIL=noreply@yourdomain.com
    ```

4. Restart: `docker compose -f docker-compose.selfhost.yml restart backend`

For more auth configuration (OAuth, signup allowlist), see [Auth setup](/auth-setup).

## 4. First login + create a workspace

Open [http://localhost:3000](http://localhost:3000):

- Enter your email
- Grab the verification code from the Resend email (or, if you haven't configured Resend, from the server container stdout — look for the `[DEV] Verification code` line)
- Log in and create your first workspace

## 5. Point the CLI at your own server

The CLI install is the same as in [Cloud quickstart → 2. Install the CLI](/cloud-quickstart#2-install-the-multica-cli) — Homebrew / script / PowerShell, pick one. Once installed, **use the self-host variant of the setup command**:

```bash
multica setup self-host --server-url http://<your-server-address>:8080 --app-url http://<your-server-address>:3000
```

If you're running everything on one local machine:

```bash
multica setup self-host
```

That defaults to `http://localhost:8080` (backend) and `http://localhost:3000` (frontend).

`setup self-host` takes you through browser login, stores the PAT locally, and **starts the daemon automatically**.

## 6. Create an agent + assign your first task

Same flow as Cloud — see [Cloud quickstart → Steps 5-6](/cloud-quickstart#5-create-an-agent).

## Common issues

- **Backend won't start**: check container logs with `docker compose -f docker-compose.selfhost.yml logs backend`; usually it's a bad `DATABASE_URL` or `JWT_SECRET` in `.env`
- **Verification code not received**: Resend isn't configured → look for `[DEV] Verification code` in `docker compose logs backend`
- **WebSocket won't connect**: for public deployments you must set `FRONTEND_ORIGIN` to your real frontend domain; see [Troubleshooting → WebSocket won't connect](/troubleshooting#websocket-wont-connect)

## Next steps

- [Environment variables](/environment-variables) — full env reference
- [Auth setup](/auth-setup) — Resend / OAuth / signup allowlist in detail
- [Troubleshooting](/troubleshooting) — start here when things go wrong
- [Desktop app](/desktop-app) — the desktop app can also connect to your self-hosted backend
