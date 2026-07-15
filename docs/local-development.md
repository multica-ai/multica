# Local development

This guide starts a fully isolated Multica environment from the current
checkout. Backend, frontend, CLI, daemon, database, workspace root, and Desktop
state all come from the current worktree and do not use the system-installed
Multica CLI or production configuration.

## Quick start

For interactive development in the main checkout:

```bash
make dev
```

For a linked worktree, `make dev` automatically uses `.env.worktree`, whose
database name and ports are derived from the worktree path.

The rest of this guide documents the reproducible automation flow used by
agents, CI-like local checks, and parallel worktrees.

## Automation process rules

Long-running processes must be detached at the shell level. Do not use an
agent tool's background-process option: that keeps the agent session alive
indefinitely.

Use a unique profile and per-worktree logs:

```bash
WORKTREE_DIR="$(basename "$PWD")"
SLUG="$(printf '%s' "$WORKTREE_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
HASH="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
OFFSET=$((HASH % 1000))
PROFILE="dev-${SLUG}-${OFFSET}"
LOG="/tmp/multica-dev-${PROFILE}.log"
DAEMON_LOG="/tmp/multica-daemon-${PROFILE}.log"
```

Never use a static profile such as `dev` or a shared log such as
`/tmp/multica-dev.log`. Parallel worktrees would otherwise share daemon state
or interleave logs. On macOS use `nohup ... &` rather than `setsid`.

## 1. Prepare the worktree and database

Generate the isolated environment without starting services:

```bash
make worktree-env >/dev/null 2>&1 || true
ENV_FILE=".env.worktree"
[ -f "$ENV_FILE" ] || ENV_FILE=".env"
```

`.env.worktree` already includes `MULTICA_DEV_VERIFICATION_CODE=888888` for
non-production automated login. If an older env file does not contain it, add
the variable and restart the backend before authenticating.

Create the database through the actual `DATABASE_URL` used by the backend:

```bash
DB_URL=$(grep '^DATABASE_URL=' "$ENV_FILE" | head -1 | cut -d= -f2-)
DB_NAME=$(printf '%s' "$DB_URL" | sed -E 's#^.*/([^/?]+)(\?.*)?$#\1#')
ADMIN_URL=$(printf '%s' "$DB_URL" | sed -E "s#/${DB_NAME}(\?|$)#/postgres\1#")

psql "$ADMIN_URL" -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" 2>/dev/null | grep -q 1 \
  || psql "$ADMIN_URL" -c "CREATE DATABASE \"${DB_NAME}\";"
```

This is intentionally not `docker compose exec`. A native PostgreSQL process
may already own `localhost:5432`; in that case Docker-side database creation
succeeds against one server while the backend connects to another. Diagnose
the listener with:

```bash
lsof -iTCP:5432 -sTCP:LISTEN -n -P
```

## 2. Start backend and frontend

Start from the current source and detach the process:

```bash
nohup make dev > "$LOG" 2>&1 &
```

Wait for the backend before continuing:

```bash
PORT=$(grep '^PORT=' "$ENV_FILE" | head -1 | cut -d= -f2)
PORT=${PORT:-8080}
SERVER="http://localhost:${PORT}"

for i in $(seq 1 45); do
  curl -sf "$SERVER/health" >/dev/null 2>&1 && break
  grep -qiE 'database .* does not exist|make: \*\*\* \[dev\]' "$LOG" 2>/dev/null \
    && { echo "make dev failed early; check $LOG"; break; }
  sleep 2
done
```

## 3. Create an automated local account

Call `send-code` once and `verify-code` once. Retry loops trigger the auth rate
limit and can invalidate an otherwise correct fixed code.

```bash
curl -s -X POST "$SERVER/auth/send-code" \
  -H 'Content-Type: application/json' \
  -d '{"email":"dev@localhost"}' >/dev/null

JWT=$(curl -s -X POST "$SERVER/auth/verify-code" \
  -H 'Content-Type: application/json' \
  -d '{"email":"dev@localhost","code":"888888"}' | jq -r '.token')

[ -n "$JWT" ] && [ "$JWT" != null ] || { echo "auth failed"; exit 1; }

PAT=$(curl -s -X POST "$SERVER/api/tokens" \
  -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' \
  -d '{"name":"auto-dev","expires_in_days":365}' | jq -r '.token')
```

If verification returns `400`, confirm the fixed code exists in the env file
and that the backend was restarted after the change. If the endpoint was
called repeatedly, wait for the rate limit or issue one fresh `send-code`
before a single verification attempt.

## 4. Create a workspace and finish onboarding

Workspace creation also creates its default Space. Mark onboarding complete so
browser navigation lands in the product rather than `/onboarding`.

```bash
WS=$(curl -s -X POST "$SERVER/api/workspaces" \
  -H "Authorization: Bearer $PAT" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Dev","slug":"dev"}' | jq -r '.id')

curl -s -X POST "$SERVER/api/me/onboarding/complete" \
  -H "Authorization: Bearer $PAT" \
  -H "X-Workspace-ID: $WS" \
  -H 'Content-Type: application/json' \
  -d '{"exit":"existing"}' >/dev/null

SPACE_KEY=$(curl -s "$SERVER/api/spaces" \
  -H "Authorization: Bearer $PAT" \
  -H "X-Workspace-ID: $WS" | jq -r '.spaces[0].key')
```

Discover the Space key from the API instead of assuming a fixed value.

## 5. Configure and start the source-built daemon

```bash
FRONTEND_PORT=$(grep '^FRONTEND_PORT=' "$ENV_FILE" | head -1 | cut -d= -f2)
FRONTEND_PORT=${FRONTEND_PORT:-3000}

CONFIG_DIR="$HOME/.multica/profiles/$PROFILE"
mkdir -p "$CONFIG_DIR"

cat > "$CONFIG_DIR/config.json" <<EOF
{
  "server_url": "$SERVER",
  "app_url": "http://localhost:${FRONTEND_PORT}",
  "token": "$PAT",
  "workspace_id": "$WS",
  "watched_workspaces": [{"id": "$WS", "name": "Dev"}]
}
EOF

nohup make cli MULTICA_ARGS="daemon start --profile $PROFILE" > "$DAEMON_LOG" 2>&1 &
sleep 3
make cli MULTICA_ARGS="daemon status --profile $PROFILE"
```

`make cli` runs the CLI from the current Go source tree. It does not call a
system-installed `multica` binary.

## 6. Headless readiness check

The frontend proxies `/auth/*` and `/api/*`, so a same-origin login verifies the
frontend, backend, cookie session, and workspace API without browser OTP UI.

Use a profile-specific cookie file:

```bash
FRONTEND="http://localhost:${FRONTEND_PORT}"
COOKIE_JAR="/tmp/multica-cookies-${PROFILE}.txt"

curl -s -c "$COOKIE_JAR" -X POST "$FRONTEND/auth/send-code" \
  -H 'Content-Type: application/json' \
  -d '{"email":"dev@localhost"}' >/dev/null
curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
  -X POST "$FRONTEND/auth/verify-code" \
  -H 'Content-Type: application/json' \
  -d '{"email":"dev@localhost","code":"888888"}' >/dev/null
curl -s -b "$COOKIE_JAR" -o /dev/null -w 'me: %{http_code}\n' \
  "$FRONTEND/api/me"
```

Use a real browser only for behavior or visual checks that require it. Navigate
to a real surface such as `/dev/space/${SPACE_KEY}`; there is no workspace root
page at `/dev`.

## Desktop testing

After the backend is healthy:

```bash
pnpm dev:desktop
```

Desktop builds the CLI from source, derives an isolated app identity and
renderer port for linked worktrees, and manages a profile named from the local
backend endpoint. When using a non-default backend port, set
`apps/desktop/.env.development.local`:

```dotenv
VITE_API_URL=http://localhost:<backend-port>
VITE_WS_URL=ws://localhost:<backend-port>/ws
```

Log in with `dev@localhost` and `888888`.

## Stop the environment

```bash
make cli MULTICA_ARGS="daemon stop --profile $PROFILE"
make stop-worktree
```

For the main checkout use `make stop`. The shared PostgreSQL container remains
running so other worktrees are not interrupted. Optional cleanup:

```bash
make db-down
make clean
rm -rf "$HOME/.multica/profiles/$PROFILE"
rm -f "/tmp/multica-cookies-${PROFILE}.txt"
```

## Isolation map

| Resource | Default installation | Worktree development |
| --- | --- | --- |
| CLI config | `~/.multica/config.json` | `~/.multica/profiles/$PROFILE/config.json` |
| Daemon state | default profile | dynamic `$PROFILE` |
| Workspace root | `~/multica_workspaces/` | profile-specific root |
| Database | configured deployment DB | `.env.worktree` database |
| Backend/frontend | default ports | worktree-derived ports |
| Desktop | primary app/profile | worktree-derived app/profile |

## Troubleshooting

- **Database does not exist:** create it through the env file's
  `DATABASE_URL`, not through Docker exec. Check who owns port 5432 with
  `lsof`.
- **Verification code 888888 is rejected:** ensure
  `MULTICA_DEV_VERIFICATION_CODE=888888`, restart the backend, and avoid retry
  loops.
- **Login lands on onboarding:** call
  `POST /api/me/onboarding/complete`, then navigate to a Space or Inbox route.
- **Logs contain another worktree's daemon:** use the dynamic profile-specific
  log paths from this guide.
- **Port conflict:** regenerate with `FORCE=1 make worktree-env` after stopping
  this worktree's processes.
- **Migration drift:** run `ENV_FILE=.env.worktree make migrate-up`.
- **CLI changes appear stale:** use `make cli` or rebuild before testing a
  packaged binary.
