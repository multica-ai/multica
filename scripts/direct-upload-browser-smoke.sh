#!/usr/bin/env bash
# Isolated, real-browser verification of presigned direct upload.
#
# Companion to `scripts/direct-upload-smoke.sh`: that one drives the CLI, this
# one drives Chromium against the web app. The browser is the only place two of
# this feature's failure modes exist at all:
#
#   * a CORS preflight the bucket refuses — no preflight in Node or jsdom;
#   * a request header the browser refuses to let a script set (`Content-Length`,
#     `Host`) — jsdom happily accepts both.
#
# So the whole point here is a real browser talking to a real S3-compatible
# server through its real CORS implementation, with a proxy in front of the API
# that rejects any request body above 1 MiB. A large file uploading through that
# arrangement is positive proof its bytes went to the bucket.
#
# What it verifies, in one Playwright test:
#   1. A PNG larger than the proxy's body limit uploads through the ordinary
#      attachment file input (no test-only affordance) and previews in place.
#   2. The uploads the API received are all ≤ 1 MiB, and the one PUT went to the
#      storage origin with the file's exact length.
#   3. The PUT carried no Authorization header, no cookie and no workspace
#      header, and did not try to set Content-Length or Host.
#   4. The preview's bytes are the uploaded bytes (SHA-256 compared in-page).
#   5. CORS is enforced both ways: an allowed origin with the signed header is
#      accepted; an unlisted header and a foreign origin are blocked.
#
# OWNERSHIP: every resource name carries a unique run id, creation is recorded in
# a ledger, and cleanup removes only what this ledger names. An existing
# database or container is a hard error, never something to reuse or overwrite.
# Nothing runs against a configured deployment, a real bucket, or production
# credentials.
#
# Usage:
#   bash scripts/direct-upload-browser-smoke.sh
#   SMOKE_KEEP=1 bash scripts/direct-upload-browser-smoke.sh   # leave the stack up
#
# Requirements: go, docker, curl, python3, node, pnpm, and installed workspace
# dependencies (pnpm install). The S3 server image is pulled on first run; set
# SMOKE_S3_IMAGE to use another S3-compatible image.
set -uo pipefail

# Overridable so the safety test can run this script's preflight from a copy
# outside the checkout: the dependency probe below must measure the real
# workspace, not the directory the copy happens to sit in.
REPO_ROOT="${SMOKE_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

RAW_RUN_ID="$(date +%s)-$$-$(od -An -N4 -tx4 /dev/urandom | tr -d ' \n')"
RUN_ID="$(printf '%s' "$RAW_RUN_ID" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed -e 's/--*/-/g' -e 's/^-//' -e 's/-$//')"
[ -n "$RUN_ID" ] || RUN_ID="run-$(date +%s)"

if [ -n "${SMOKE_DIR:-}" ]; then
  if [ -d "$SMOKE_DIR" ] && [ -n "$(ls -A "$SMOKE_DIR" 2>/dev/null)" ]; then
    printf '\033[31m✗ SMOKE_DIR %s is not empty; refusing to use it\033[0m\n' "$SMOKE_DIR" >&2
    exit 2
  fi
  mkdir -p "$SMOKE_DIR"
  SMOKE_KEEP_DIR=1
else
  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/multica-browser-upload-smoke.XXXXXX")" || exit 2
  SMOKE_KEEP_DIR=0
fi
LOG_DIR="$SMOKE_DIR/logs"
mkdir -p "$LOG_DIR"

DB_NAME="multica_browser_${RUN_ID//-/_}"
DB_NAME="${DB_NAME:0:60}"
S3_CONTAINER="multica-browser-s3-${RUN_ID}"
S3_BUCKET="multica-browser-${RUN_ID}"
S3_IMAGE="${SMOKE_S3_IMAGE:-rustfs/rustfs:latest}"
S3_ACCESS_KEY="browsersmoke"
S3_SECRET_KEY="browsersmoke123"
SMOKE_EMAIL="browser-${RUN_ID}@smoke.local"
SMOKE_SLUG="browser-${RUN_ID}"
SMOKE_SLUG="${SMOKE_SLUG:0:40}"

OWNED_FILE="$SMOKE_DIR/owned.tsv"
: > "$OWNED_FILE"
own() { printf '%s\t%s\n' "$1" "$2" >> "$OWNED_FILE"; }
we_own() { [ -f "$OWNED_FILE" ] && awk -F'\t' -v k="$1" -v v="$2" '$1==k && $2==v {found=1} END {exit !found}' "$OWNED_FILE"; }
owned_of_kind() { awk -F'\t' -v k="$1" '$1==k {print $2}' "$OWNED_FILE" 2>/dev/null; }

PASS=0
FAIL=0
declare -a FAILURES=()
step() { printf '\n==> %s\n' "$1"; }
info() { printf '    %s\n' "$1"; }
ok()   { PASS=$((PASS + 1)); printf '    \033[32m✓\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '    \033[31m✗\033[0m %s\n' "$1"; }
die()  { printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

free_port() {
  local port=$1
  if command -v ss >/dev/null 2>&1; then
    ! ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}\$"
  else
    ! (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null
  fi
}
pick_port() {
  local base=$1 candidate
  for offset in $(seq 0 200); do
    candidate=$((base + offset))
    if free_port "$candidate"; then printf '%s' "$candidate"; return 0; fi
  done
  return 1
}

cleanup() {
  local status=$?
  step "Cleanup"
  [ -n "${WEB_PID:-}" ] && kill "$WEB_PID" 2>/dev/null
  [ -n "${PROXY_PID:-}" ] && kill "$PROXY_PID" 2>/dev/null
  [ -n "${API_PID:-}" ] && kill "$API_PID" 2>/dev/null

  if [ "${SMOKE_KEEP:-0}" = "1" ]; then
    info "SMOKE_KEEP=1: leaving this run's stack up"
    info "  run id:   $RUN_ID"
    info "  web:      http://127.0.0.1:${WEB_PORT:-?}"
    info "  API:      http://127.0.0.1:${API_PORT:-?}"
    info "  storage:  http://127.0.0.1:${S3_PORT:-?}"
    info "  database: $DB_NAME (on ${PG_CONTAINER:-?})"
    info "  logs:     $LOG_DIR"
    info "  stop:     kill ${WEB_PID:-} ${PROXY_PID:-} ${API_PID:-}; docker rm -f $S3_CONTAINER"
    info "            docker exec ${PG_CONTAINER:-<pg>} psql -U ${POSTGRES_USER:-multica} -d postgres -c 'DROP DATABASE \"$DB_NAME\"'"
    exit $status
  fi

  local container
  while read -r container; do
    [ -n "$container" ] || continue
    if we_own container "$container"; then
      docker rm -f "$container" >/dev/null 2>&1 && info "removed container $container (created by this run)"
    fi
  done <<EOF
$(owned_of_kind container)
EOF

  local database
  while read -r database; do
    [ -n "$database" ] || continue
    if we_own database "$database" && [ -n "${PG_CONTAINER:-}" ]; then
      docker exec "$PG_CONTAINER" psql -U "${POSTGRES_USER:-multica}" -d postgres \
        -c "DROP DATABASE IF EXISTS \"$database\"" >/dev/null 2>&1 \
        && info "dropped database $database (created by this run)"
    fi
  done <<EOF
$(owned_of_kind database)
EOF

  if [ "$SMOKE_KEEP_DIR" = "1" ]; then
    info "SMOKE_DIR was supplied by the caller; leaving it in place: $SMOKE_DIR"
  else
    chmod -R u+rwX,o+rwX "$SMOKE_DIR/s3" 2>/dev/null
    rm -rf "$SMOKE_DIR" 2>/dev/null
  fi

  if [ "$FAIL" -gt 0 ]; then
    printf '\n\033[31mFAILED: %d check(s)\033[0m\n' "$FAIL"
    for failure in "${FAILURES[@]}"; do printf '  - %s\n' "$failure"; done
    exit 1
  fi
  printf '\n\033[32mALL CHECKS PASSED (%d)\033[0m\n' "$PASS"
  exit $status
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- preflight --

step "Preflight"
for tool in go docker curl python3 awk od node pnpm; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done
[ -d "$REPO_ROOT/node_modules" ] || die "workspace dependencies are missing; run 'pnpm install' in $REPO_ROOT first"
[ -x "$REPO_ROOT/node_modules/.bin/playwright" ] || die "Playwright is not installed; run 'pnpm install' first"
info "run id:  $RUN_ID"
info "workdir: $SMOKE_DIR"

PG_CONTAINER="${SMOKE_PG_CONTAINER:-}"
if [ -z "$PG_CONTAINER" ]; then
  PG_CONTAINER="$(cd "$REPO_ROOT" && docker compose ps -q postgres 2>/dev/null | head -1)"
  if [ -n "$PG_CONTAINER" ]; then
    PG_CONTAINER="$(docker inspect -f '{{.Name}}' "$PG_CONTAINER" 2>/dev/null | sed 's#^/##')"
  fi
fi
if [ -z "$PG_CONTAINER" ]; then
  die "no PostgreSQL server was found for this checkout.
    Run 'make up C=api' here, or pass SMOKE_PG_CONTAINER=<container-name> explicitly.
    This script will not guess, because a wrong guess can drop someone else's database."
fi
docker inspect "$PG_CONTAINER" >/dev/null 2>&1 \
  || die "PostgreSQL container '$PG_CONTAINER' does not exist; refusing to continue"
docker inspect -f '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null | grep -q true \
  || die "PostgreSQL container '$PG_CONTAINER' is not running"
POSTGRES_USER="$(docker exec "$PG_CONTAINER" printenv POSTGRES_USER 2>/dev/null || echo multica)"
POSTGRES_PASSWORD="$(docker exec "$PG_CONTAINER" printenv POSTGRES_PASSWORD 2>/dev/null || echo multica)"
ok "using PostgreSQL server: $PG_CONTAINER"

existing_db="$(docker exec "$PG_CONTAINER" psql -U "$POSTGRES_USER" -d postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" 2>/dev/null)"
[ -z "$existing_db" ] || die "database '$DB_NAME' already exists; refusing to touch it"
docker exec "$PG_CONTAINER" psql -U "$POSTGRES_USER" -d postgres \
  -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$DB_NAME\"" >/dev/null \
  || die "could not create database $DB_NAME"
own database "$DB_NAME"
ok "created database $DB_NAME (recorded as owned by this run)"

S3_PORT="$(pick_port "${SMOKE_S3_PORT:-29200}")" || die "no free storage port"
API_PORT="$(pick_port $((S3_PORT + 1)))" || die "no free API port"
PROXY_PORT="$(pick_port $((S3_PORT + 2)))" || die "no free proxy port"
WEB_PORT="$(pick_port $((S3_PORT + 3)))" || die "no free web port"
S3_ENDPOINT="http://127.0.0.1:${S3_PORT}"
# `localhost`, not 127.0.0.1: Next 16 refuses to serve its dev resources (the
# hydration scripts themselves) to a "cross-origin" host it does not recognise,
# and the app never hydrates. `localhost` is also what a user actually opens.
WEB_ORIGIN="http://localhost:${WEB_PORT}"

# --------------------------------------------------------- object storage ----

step "Object storage with a CORS rule for the web origin"
if docker inspect "$S3_CONTAINER" >/dev/null 2>&1; then
  die "container '$S3_CONTAINER' already exists; refusing to remove or reuse it"
fi
mkdir -p "$SMOKE_DIR/s3/data" "$SMOKE_DIR/s3/logs"
chmod -R 0777 "$SMOKE_DIR/s3"
if ! docker image inspect "$S3_IMAGE" >/dev/null 2>&1; then
  info "pulling $S3_IMAGE"
  docker pull "$S3_IMAGE" >/dev/null 2>&1 || die "could not pull $S3_IMAGE"
fi
docker run -d --name "$S3_CONTAINER" \
  -p "127.0.0.1:${S3_PORT}:9000" \
  -e RUSTFS_ACCESS_KEY="$S3_ACCESS_KEY" \
  -e RUSTFS_SECRET_KEY="$S3_SECRET_KEY" \
  -v "$SMOKE_DIR/s3/data:/data" \
  -v "$SMOKE_DIR/s3/logs:/logs" \
  "$S3_IMAGE" >/dev/null || die "could not start $S3_IMAGE"
own container "$S3_CONTAINER"
for _ in $(seq 1 40); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$S3_ENDPOINT/" || true)"
  [ "$code" = "403" ] && break
  sleep 0.5
done
[ "$code" = "403" ] || die "object storage did not become ready (last status $code)"
ok "object storage ready at $S3_ENDPOINT"

# Bucket + CORS with the backend's own SDK, so the run needs no extra CLI.
mkdir -p "$SMOKE_DIR/mkbucket"
cat > "$SMOKE_DIR/mkbucket/main.go" <<'GO'
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// usage: mkbucket <endpoint> <access> <secret> <bucket> <allowed-origin>
func main() {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Args[2], os.Args[3], "")),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(os.Args[1])
		o.UsePathStyle = true
	})
	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(os.Args[4])}); err != nil {
		fmt.Fprintln(os.Stderr, "create bucket:", err)
		os.Exit(1)
	}
	// Exactly the rule the docs prescribe for a browser deployment: the app's
	// origin, PUT/GET/HEAD, and only the content-type header the credential
	// signs. A wider rule here would make the "unlisted header is blocked"
	// assertion meaningless.
	_, err = client.PutBucketCors(context.Background(), &s3.PutBucketCorsInput{
		Bucket: aws.String(os.Args[4]),
		CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
			AllowedOrigins: []string{strings.TrimSpace(os.Args[5])},
			AllowedMethods: []string{"PUT", "GET", "HEAD"},
			AllowedHeaders: []string{"content-type"},
			ExposeHeaders:  []string{"ETag"},
			MaxAgeSeconds:  aws.Int32(300),
		}}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "put bucket cors:", err)
		os.Exit(1)
	}
}
GO
( cd "$REPO_ROOT/server" && go run "$SMOKE_DIR/mkbucket/main.go" "$S3_ENDPOINT" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" "$S3_BUCKET" "$WEB_ORIGIN" ) \
  >"$LOG_DIR/mkbucket.log" 2>&1 || die "could not create the bucket (see $LOG_DIR/mkbucket.log)"
ok "created bucket $S3_BUCKET with a CORS rule for $WEB_ORIGIN"

# ------------------------------------------------------------------- API -----

step "Backend against the local bucket"
cd "$REPO_ROOT/server" || die "server directory missing"
DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/${DB_NAME}?sslmode=disable" \
  go run ./cmd/migrate up >"$LOG_DIR/migrate.log" 2>&1 \
  || die "migrations failed (see $LOG_DIR/migrate.log)"
ok "migrations applied"

export DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/${DB_NAME}?sslmode=disable"
export PORT="$API_PORT"
export APP_ENV=development
export JWT_SECRET="browser-upload-smoke-secret"
export MULTICA_DEV_VERIFICATION_CODE=888888
export MULTICA_PUBLIC_URL="http://127.0.0.1:${API_PORT}"
export MULTICA_APP_URL="$WEB_ORIGIN"
export FRONTEND_ORIGIN="$WEB_ORIGIN"
export CORS_ALLOWED_ORIGINS="$WEB_ORIGIN"
export S3_BUCKET="$S3_BUCKET"
export S3_REGION=us-east-1
export AWS_ENDPOINT_URL="$S3_ENDPOINT"
export S3_USE_PATH_STYLE=true
export AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY"
export ATTACHMENT_DOWNLOAD_MODE=presign
export ALLOW_SIGNUP=true

# The agent runtime exports these pointing at production; a server started with
# them behaves as if it were inside a task.
unset MULTICA_AGENT_ID MULTICA_TASK_ID MULTICA_DAEMON_PORT MULTICA_TASK_SLOT \
      MULTICA_TASK_CONFIG_ROOT MULTICA_TASK_WORKSPACES_ROOT MULTICA_WORKSPACES_ROOT

go build -o "$SMOKE_DIR/multica-server" ./cmd/server >"$LOG_DIR/build-api.log" 2>&1 \
  || die "backend build failed (see $LOG_DIR/build-api.log)"
"$SMOKE_DIR/multica-server" >"$LOG_DIR/api.log" 2>&1 &
API_PID=$!
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:${API_PORT}/health" >/dev/null 2>&1; then break; fi
  kill -0 "$API_PID" 2>/dev/null || die "backend exited during startup (see $LOG_DIR/api.log)"
  sleep 1
done
curl -sf "http://127.0.0.1:${API_PORT}/health" >/dev/null 2>&1 || die "backend never became healthy"
ok "backend healthy at http://127.0.0.1:${API_PORT}"

# ---------------------------------------------------- 1 MiB API body proxy ----

step "Reverse proxy with a 1 MiB request-body limit in front of the API"
# Same shape as the CLI smoke's proxy and for the same reason: the deployment
# this feature exists for sits behind a body limit, so the browser must be able
# to finish an upload through it. Bodies are counted as they are forwarded, which
# is what proves the file never travelled this way.
cat > "$SMOKE_DIR/proxy.py" <<'PY'
"""A reverse proxy that rejects any request body above a fixed limit.

Small enough to need nothing but the standard library, and it records the size
of every body it forwards so the caller can assert on what the API actually saw.
"""
import asyncio
import sys

LIMIT = int(sys.argv[2])
UPSTREAM_PORT = int(sys.argv[3])
METRICS_PATH = sys.argv[4]


async def handle(reader, writer):
    try:
        request_line = await reader.readline()
        if not request_line:
            return
        headers = []
        length = 0
        while True:
            line = await reader.readline()
            if line in (b"\r\n", b"\n", b""):
                break
            headers.append(line)
            name, _, value = line.decode("latin-1").partition(":")
            if name.strip().lower() == "content-length":
                length = int(value.strip() or 0)

        if length > LIMIT:
            with open(METRICS_PATH, "a") as metrics:
                metrics.write("rejected\t%d\t%s\n" % (length, request_line.decode("latin-1").split(" ")[1]))
            body = b"413 Request Entity Too Large"
            writer.write(
                b"HTTP/1.1 413 Request Entity Too Large\r\n"
                b"Content-Type: text/plain\r\n"
                b"Content-Length: " + str(len(body)).encode() + b"\r\n"
                b"Connection: close\r\n\r\n" + body
            )
            await writer.drain()
            return

        upstream_reader, upstream_writer = await asyncio.open_connection("127.0.0.1", UPSTREAM_PORT)
        upstream_writer.write(request_line)
        for line in headers:
            upstream_writer.write(line)
        upstream_writer.write(b"\r\n")

        forwarded = 0
        remaining = length
        while remaining > 0:
            chunk = await reader.read(min(remaining, 65536))
            if not chunk:
                break
            remaining -= len(chunk)
            forwarded += len(chunk)
            upstream_writer.write(chunk)
            await upstream_writer.drain()
        with open(METRICS_PATH, "a") as metrics:
            metrics.write("forwarded\t%d\t%s\n" % (forwarded, request_line.decode("latin-1").split(" ")[1]))

        status_line = await upstream_reader.readline()
        response_headers = []
        response_length = 0
        chunked = False
        while True:
            line = await upstream_reader.readline()
            if line in (b"\r\n", b"\n", b""):
                break
            response_headers.append(line)
            lowered = line.decode("latin-1").lower()
            if lowered.startswith("content-length:"):
                response_length = int(lowered.split(":", 1)[1].strip())
            if lowered.startswith("transfer-encoding:") and "chunked" in lowered:
                chunked = True
        writer.write(status_line)
        for line in response_headers:
            if line.lower().startswith((b"connection:", b"keep-alive:")):
                continue
            writer.write(line)
        writer.write(b"Connection: close\r\n\r\n")
        if chunked:
            while True:
                size_line = await upstream_reader.readline()
                writer.write(size_line)
                size = int(size_line.strip().split(b";")[0] or b"0", 16)
                if size == 0:
                    writer.write(await upstream_reader.readline())
                    break
                writer.write(await upstream_reader.readexactly(size))
                writer.write(await upstream_reader.readexactly(2))
        elif response_length:
            writer.write(await upstream_reader.readexactly(response_length))
        await writer.drain()
        upstream_writer.close()
    except Exception:  # noqa: BLE001 - a dropped connection is not a test failure
        pass
    finally:
        try:
            writer.close()
        except Exception:  # noqa: BLE001
            pass


async def main():
    server = await asyncio.start_server(handle, "127.0.0.1", int(sys.argv[1]))
    async with server:
        await server.serve_forever()


asyncio.run(main())
PY
PROXY_METRICS="$SMOKE_DIR/proxy-metrics.tsv"
: > "$PROXY_METRICS"
python3 "$SMOKE_DIR/proxy.py" "$PROXY_PORT" $((1 << 20)) "$API_PORT" "$PROXY_METRICS" \
  >"$LOG_DIR/proxy.log" 2>&1 &
PROXY_PID=$!
for _ in $(seq 1 40); do
  curl -sf "http://127.0.0.1:${PROXY_PORT}/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf "http://127.0.0.1:${PROXY_PORT}/health" >/dev/null 2>&1 \
  || die "proxy never reached the backend"
ok "proxy on :$PROXY_PORT forwards to :$API_PORT (body limit 1 MiB)"

# The control: the legacy multipart route must be refused by this proxy. Without
# it, "the large upload succeeded" would not prove the direct path was taken.
CONTROL_FILE="$SMOKE_DIR/control-2m.bin"
head -c $((2 << 20)) /dev/urandom > "$CONTROL_FILE"
CONTROL_STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${PROXY_PORT}/api/upload-file" \
  -H "Content-Type: multipart/form-data" --data-binary "@$CONTROL_FILE")"
if [ "$CONTROL_STATUS" = "413" ]; then
  ok "control: a 2 MiB request body is rejected by the proxy with 413"
else
  bad "control: expected 413 from the proxy for a 2 MiB body, got $CONTROL_STATUS"
fi

# ------------------------------------------------------------------- web -----

step "Web app in front of the proxy (same-origin, as a real deployment)"
# Only REMOTE_API_URL is set: the browser then talks to its own origin and the
# Next.js proxy forwards /api and /ws. That is what makes the app's requests
# same-origin, and it is also what a self-hosted deployment does.
( cd "$REPO_ROOT/apps/web" && \
  env -u NEXT_PUBLIC_API_URL \
    REMOTE_API_URL="http://127.0.0.1:${PROXY_PORT}" \
    NEXT_PUBLIC_WS_URL="ws://127.0.0.1:${WEB_PORT}/ws" \
    FRONTEND_ORIGIN="$WEB_ORIGIN" \
    FRONTEND_PORT="$WEB_PORT" \
    PORT="$WEB_PORT" \
    pnpm exec next dev --port "$WEB_PORT" >"$LOG_DIR/web.log" 2>&1 & )
sleep 2
WEB_PID="$(pgrep -f "next dev --port ${WEB_PORT}" | head -1)"
[ -n "$WEB_PID" ] || die "the web dev server did not start (see $LOG_DIR/web.log)"
for _ in $(seq 1 120); do
  curl -sf "$WEB_ORIGIN/login" >/dev/null 2>&1 && break
  sleep 1
done
curl -sf "$WEB_ORIGIN/login" >/dev/null 2>&1 || die "the web app never became ready (see $LOG_DIR/web.log)"
ok "web app ready at $WEB_ORIGIN (pid $WEB_PID)"

# The same-origin proxy must reach the API before the browser is asked to.
CONFIG_STATUS="$(curl -s -o /dev/null -w '%{http_code}' "$WEB_ORIGIN/api/config")"
[ "$CONFIG_STATUS" = "200" ] || die "the web app's /api proxy did not reach the backend (status $CONFIG_STATUS)"
ok "the web app proxies /api to the backend"

# --------------------------------------------------------------- browser ----

step "Real browser: upload, preview, download and CORS enforcement"
BROWSER_LOG="$LOG_DIR/playwright.log"
# An explicit browser binary wins over the downloaded one, so a machine that has
# a different Chromium on disk need not download another revision. CORS and the
# forbidden-header list are the engine's, not one build's.
CHROME_ENV=""
if [ -n "${SMOKE_CHROME_PATH:-}" ]; then
  [ -x "$SMOKE_CHROME_PATH" ] || die "SMOKE_CHROME_PATH is not executable: $SMOKE_CHROME_PATH"
  CHROME_ENV="DIRECT_UPLOAD_CHROME_PATH=$SMOKE_CHROME_PATH"
  info "using browser: $SMOKE_CHROME_PATH"
fi
# Refuse to run a browser suite we cannot launch: a skipped suite reads like a
# pass in the log, and this script's whole value is the browser assertion.
if [ -z "$CHROME_ENV" ] && ! ( cd "$REPO_ROOT" && pnpm exec playwright install --dry-run chromium >/dev/null 2>&1 ); then
  info "no Playwright Chromium is installed; downloading it now"
  ( cd "$REPO_ROOT" && pnpm exec playwright install chromium ) >"$LOG_DIR/playwright-install.log" 2>&1 \
    || die "could not install the Playwright browser (see $LOG_DIR/playwright-install.log)"
fi

( cd "$REPO_ROOT" && \
  env $CHROME_ENV \
    DIRECT_UPLOAD_SMOKE_DIR="$SMOKE_DIR" \
    DIRECT_UPLOAD_WEB_ORIGIN="$WEB_ORIGIN" \
    DIRECT_UPLOAD_STORAGE_ORIGIN="$S3_ENDPOINT" \
    DIRECT_UPLOAD_FOREIGN_ORIGIN="http://127.0.0.1:${API_PORT}" \
    DIRECT_UPLOAD_SMOKE_EMAIL="$SMOKE_EMAIL" \
    DIRECT_UPLOAD_SMOKE_SLUG="$SMOKE_SLUG" \
    PLAYWRIGHT_BASE_URL="$WEB_ORIGIN" \
    MULTICA_DEV_VERIFICATION_CODE=888888 \
    pnpm exec playwright test e2e/direct-upload-browser.spec.ts --project=chromium --workers=1 --retries=0 \
  ) >"$BROWSER_LOG" 2>&1
BROWSER_STATUS=$?
if [ $BROWSER_STATUS -eq 0 ]; then
  # A skip exits 0 too, and a skipped browser check would make this whole script
  # a no-op that reports success.
  if grep -qE '[0-9]+ skipped' "$BROWSER_LOG"; then
    bad "the browser spec was skipped, so nothing was verified (see $BROWSER_LOG)"
  else
    ok "the browser spec passed (see $BROWSER_LOG)"
  fi
else
  bad "the browser spec failed (see $BROWSER_LOG)"
  tail -40 "$BROWSER_LOG" | sed 's/^/      /'
fi

# The proxy's own accounting: no API request may have carried a file.
MAX_FORWARDED="$(awk -F'\t' '$1=="forwarded" {if ($2+0 > m) m = $2+0} END {print m+0}' "$PROXY_METRICS")"
if [ "$MAX_FORWARDED" -le $((1 << 20)) ]; then
  ok "largest API request body the proxy forwarded: ${MAX_FORWARDED} bytes (≤ 1 MiB)"
else
  bad "the proxy forwarded a ${MAX_FORWARDED}-byte body to the API"
fi
REJECTED="$(awk -F'\t' '$1=="rejected"' "$PROXY_METRICS" | wc -l | tr -d ' ')"
info "proxy refused ${REJECTED} oversized request(s) (the control above)"

# --------------------------------------------------------------- report -----

step "Summary"
info "run id:  $RUN_ID"
info "logs:    $LOG_DIR"
info "evidence: $SMOKE_DIR/browser-upload.png (uploaded fixture)"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
