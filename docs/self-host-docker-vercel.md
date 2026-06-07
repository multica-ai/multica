# Vercel Frontend + Docker Backend Self-Hosting

This guide covers the deployment shape where the workspace frontend runs on Vercel and the Go backend runs as a Docker container on your own server.

## Scope

- Frontend: Vercel builds `apps/workspace`.
- Backend: your server builds this repository with `docker build`.
- Database: managed separately, for example by 1Panel PostgreSQL on an external Docker network.
- The deploy script only manages the backend container and database migrations.

## First-time server setup

Clone the repository on the server:

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
```

Create a production env file:

```bash
cp deploy/docker-server.env.example deploy/docker-server.env
```

Edit `deploy/docker-server.env` and fill at least:

- `DATABASE_URL`
- `JWT_SECRET`
- `FRONTEND_ORIGIN`
- `CORS_ALLOWED_ORIGINS`
- `MULTICA_APP_URL`
- `RESEND_API_KEY`
- `RESEND_FROM_EMAIL`

Do not commit `deploy/docker-server.env`. It contains production secrets and is ignored by git.

## Deploy

Run the backend deployment from the repository root:

```bash
bash scripts/deploy-docker-server.sh
```

The default settings match a 1Panel-style setup:

- image: `multim-server`
- container: `multim-server`
- Docker network: `1panel-network`
- host port: `10626`
- container port: `8080`
- env file: `deploy/docker-server.env`

Override any setting from the shell:

```bash
ENV_FILE=/opt/multica/server.env \
DOCKER_NETWORK=1panel-network \
HOST_PORT=10626 \
bash scripts/deploy-docker-server.sh
```

The script runs in this order:

1. Validate Docker, git, curl, Docker network, and the env file.
2. Run `git pull --ff-only`, unless `SKIP_PULL=1`.
3. Build `multim-server:<commit>` and `multim-server:latest`.
4. Run `./migrate up` from the new image.
5. Replace the old backend container.
6. Poll `http://127.0.0.1:<HOST_PORT>/health`.
7. Prune dangling Docker image layers.

If migration fails, the old container is left running because replacement happens only after migrations succeed.

## Dry run and validation

Validate the env file and local tools without building or replacing containers:

```bash
VALIDATE_ONLY=1 bash scripts/deploy-docker-server.sh
```

Print the commands without executing Docker or git changes:

```bash
DRY_RUN=1 SKIP_PULL=1 bash scripts/deploy-docker-server.sh
```

## Vercel settings

Set these environment variables in the Vercel project:

```bash
VITE_API_URL=https://api.example.com
VITE_WS_URL=wss://api.example.com/ws
```

Use your backend public HTTPS domain. The backend env should point back to the frontend domain:

```bash
FRONTEND_ORIGIN=https://app.example.com
CORS_ALLOWED_ORIGINS=https://app.example.com
MULTICA_APP_URL=https://app.example.com
GOOGLE_REDIRECT_URI=https://app.example.com/auth/callback
```

## Logs and health checks

Check the running container:

```bash
docker ps --filter name=multim-server
docker logs -f multim-server
curl -fsS http://127.0.0.1:10626/health
```

## Rollback

List available local image tags:

```bash
docker images multim-server
```

Start an older image tag:

```bash
docker rm -f multim-server
docker run -d \
  --name multim-server \
  --restart unless-stopped \
  --network 1panel-network \
  -p 10626:8080 \
  --env-file deploy/docker-server.env \
  multim-server:<old-tag>
```

Application rollback does not roll back database migrations. Review migration changes before deploying releases that alter schema.

## Secret rotation

If a production `DATABASE_URL`, `JWT_SECRET`, `RESEND_API_KEY`, or OAuth secret was pasted into chat, logs, shell history, or committed files, rotate it before the next deployment.
