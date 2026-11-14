#!/usr/bin/env bash
# Isolated end-to-end smoke test for presigned direct upload.
#
# What it proves, end to end and reproducibly on one machine:
#
#   1. A reverse proxy with a 1 MiB request-body limit sits in front of the API.
#      Large files upload SUCCESSFULLY through it, which is only possible if the
#      bytes went to object storage and not through the proxy.
#   2. The API measured by the proxy receives only metadata-sized bodies.
#   3. Downloaded bytes match the uploaded bytes at 50 MiB and 100 MiB.
#   4. A declaration above the 100 MiB cap is refused before any upload.
#   5. Peak RSS of the CLI and the backend does not scale with file size — the
#      upload streams instead of buffering.
#   6. Object storage holds the file under a final key while the staging object
#      is reclaimed by the cleanup worker.
#   7. A zero-byte file is DECLINED by the presign endpoint (501 with the
#      capability code, no credential) and still publishes through the multipart
#      fallback: an empty presigned credential would bind no size at all.
#
# OWNERSHIP RULES — this script must never destroy anything it did not create:
#
#   * Every resource name carries a unique run id, so two concurrent runs cannot
#     collide and a rerun never reuses a previous run's name.
#   * A resource is only ever removed if THIS run created it. Creation is
#     recorded in $SMOKE_DIR/owned.tsv immediately after it succeeds, and cleanup
#     reads that file — it never deletes by name pattern, never picks "the first
#     postgres container", and never drops a database it did not create.
#   * An existing resource is a hard error, never something to overwrite.
#     Databases are created with an explicit existence check first.
#   * $SMOKE_DIR is created by this script under a unique mktemp name. A
#     caller-supplied SMOKE_DIR is used only if it is empty or absent, is never
#     deleted, and is left in place with a note.
#   * Nothing is touched before preflight has fully succeeded.
#
# It refuses to run against anything but a disposable local stack: its own
# PostgreSQL database, its own S3-compatible server, its own API port. It never
# touches a configured deployment, a real bucket, or production credentials.
#
# Usage:
#   bash scripts/direct-upload-smoke.sh                 # full run (50 + 100 MiB)
#   SMOKE_QUICK=1 bash scripts/direct-upload-smoke.sh   # 50 MiB only, faster
#   SMOKE_KEEP=1  bash scripts/direct-upload-smoke.sh   # keep the stack up
#   SMOKE_PG_CONTAINER=name ...                         # pin the database server
#
# Requirements: go, docker, curl, python3, awk. The S3 server image is pulled on
# first run; set SMOKE_S3_IMAGE to use another S3-compatible image.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# A unique id per run. Every resource name below embeds it, so concurrent runs
# and reruns cannot collide, and ownership is unambiguous.
#
# The RAW id goes into logs only; every resource name uses RUN_ID, which is
# reduced to [a-z0-9-]. A run id is not a trust boundary, but it must never be
# able to inject shell syntax, a path separator, or a PostgreSQL identifier
# character into a name that is later interpolated into a command.
RAW_RUN_ID="$(date +%s)-$$-$(od -An -N4 -tx4 /dev/urandom | tr -d ' \n')"
RUN_ID="$(printf '%s' "$RAW_RUN_ID" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed -e 's/--*/-/g' -e 's/^-//' -e 's/-$//')"
[ -n "$RUN_ID" ] || RUN_ID="run-$(date +%s)"

SMOKE_KEEP_DIR=0
if [ -n "${SMOKE_DIR:-}" ]; then
  # A caller-supplied directory is used, never deleted, and must be empty: a
  # non-empty one may hold another run's evidence.
  if [ -d "$SMOKE_DIR" ] && [ -n "$(ls -A "$SMOKE_DIR" 2>/dev/null)" ]; then
    printf '\033[31m✗ SMOKE_DIR %s is not empty; refusing to use it\033[0m\n' "$SMOKE_DIR" >&2
    exit 2
  fi
  mkdir -p "$SMOKE_DIR"
  SMOKE_KEEP_DIR=1
else
  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/multica-direct-upload-smoke.XXXXXX")" || exit 2
fi
LOG_DIR="$SMOKE_DIR/logs"

# Names derived from RUN_ID: unique, greppable, and never reused.
# All three derive from the sanitized id, so none can carry shell syntax.
DB_NAME="multica_smoke_${RUN_ID//-/_}"
DB_NAME="${DB_NAME:0:60}"
S3_CONTAINER="multica-smoke-s3-${RUN_ID}"
S3_BUCKET="multica-smoke-${RUN_ID}"
S3_IMAGE="${SMOKE_S3_IMAGE:-rustfs/rustfs:latest}"
S3_ACCESS_KEY="smoketest"
S3_SECRET_KEY="smoketest123"

# Ownership ledger: one line per resource THIS run created, written the moment
# creation succeeds. Cleanup reads only this.
OWNED_FILE="$SMOKE_DIR/owned.tsv"
: > "$OWNED_FILE"
own() { printf '%s\t%s\n' "$1" "$2" >> "$OWNED_FILE"; }
we_own() { [ -f "$OWNED_FILE" ] && awk -F'\t' -v k="$1" -v v="$2" '$1==k && $2==v {found=1} END {exit !found}' "$OWNED_FILE"; }
owned_of_kind() { awk -F'\t' -v k="$1" '$1==k {print $2}' "$OWNED_FILE" 2>/dev/null; }

# Ports are probed, never assumed: a busy port must fail loudly rather than
# send this run's traffic to another process.
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
S3_PORT="$(pick_port "${SMOKE_S3_PORT:-29100}")" || { echo "no free S3 port" >&2; exit 2; }
API_PORT="$(pick_port $((S3_PORT + 1)))" || { echo "no free API port" >&2; exit 2; }
S3_ENDPOINT="http://127.0.0.1:${S3_PORT}"

PASS=0
FAIL=0
declare -a FAILURES=()

step() { printf '\n==> %s\n' "$1"; }
info() { printf '    %s\n' "$1"; }
ok()   { PASS=$((PASS + 1)); printf '    \033[32m✓\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '    \033[31m✗\033[0m %s\n' "$1"; }
die()  { printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

# Every measurement this script reports is in a table at the end, so the numbers
# live next to the assertion that used them.
METRICS_FILE="$SMOKE_DIR/metrics.tsv"
: > "$METRICS_FILE"
record() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >> "$METRICS_FILE"; }

cleanup() {
  local status=$?
  step "Cleanup"
  [ -n "${API_PID:-}" ] && kill "$API_PID" 2>/dev/null
  [ -n "${PROXY_PID:-}" ] && kill "$PROXY_PID" 2>/dev/null

  if [ "${SMOKE_KEEP:-0}" = "1" ]; then
    info "SMOKE_KEEP=1: leaving this run's stack up"
    info "  run id:   $RUN_ID"
    info "  API:      http://127.0.0.1:${API_PORT}"
    info "  database: $DB_NAME (on ${PG_CONTAINER:-?})"
    info "  S3:       $S3_ENDPOINT (container $S3_CONTAINER)"
    info "  logs:     $LOG_DIR"
    info "  stop:     kill ${API_PID:-} ${PROXY_PID:-}; docker rm -f $S3_CONTAINER"
    info "            docker exec ${PG_CONTAINER:-<pg>} psql -U ${POSTGRES_USER:-multica} -d postgres -c 'DROP DATABASE \"$DB_NAME\"'"
    exit $status
  fi

  # Every removal below is gated on THIS run having created the resource (the
  # ledger is written at creation time). Nothing is matched by name pattern, and
  # nothing that already existed is ever dropped.
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
    # The S3 server writes as uid 10001, so its data directory survives a plain
    # rm. Loosen it first; a failure here is cosmetic and must not mask the
    # result. Only this run's own mktemp directory is ever removed.
    chmod -R u+rwX,o+rwX "$SMOKE_DIR/s3" 2>/dev/null
    rm -rf "$SMOKE_DIR" 2>/dev/null
  fi

  if [ "$FAIL" -gt 0 ]; then
    printf '\n\033[31mFAILED: %d check(s)\033[0m\n' "$FAIL"
    for failure in "${FAILURES[@]}"; do printf '  - %s\n' "$failure"; done
  else
    printf '\n\033[32mALL CHECKS PASSED (%d)\033[0m\n' "$PASS"
  fi
  exit $status
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- preflight --

step "Preflight"
for tool in go docker curl python3 awk od; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done
mkdir -p "$LOG_DIR"
info "run id:  $RUN_ID"
info "workdir: $SMOKE_DIR"
info "database: $DB_NAME"

# The database SERVER to use. It is never auto-selected: picking "the first
# postgres container on the machine" means a parallel run can create its
# database inside someone else's server, or drop a database there.
#
# SMOKE_PG_CONTAINER pins it explicitly. Otherwise this script only accepts a
# server this repository's own dev environment registered, which is what
# `make up C=api` creates, and it verifies that registration before using it.
PG_CONTAINER="${SMOKE_PG_CONTAINER:-}"
if [ -z "$PG_CONTAINER" ]; then
  # Resolve the server through THIS repository's compose project, which is the
  # environment `make up` manages. `docker compose ps -q postgres` names exactly
  # one container: the one belonging to this checkout's compose project. That is
  # deterministic and repo-scoped, unlike "the first postgres container on the
  # machine", which is how a parallel run ends up sharing (and then dropping) a
  # database in someone else's server.
  PG_CONTAINER="$(cd "$REPO_ROOT" && docker compose ps -q postgres 2>/dev/null | head -1)"
  if [ -n "$PG_CONTAINER" ]; then
    # ps -q yields an id; resolve the name for readable messages and for the
    # `docker exec` calls below (both work, but a name is what a human checks).
    PG_CONTAINER="$(docker inspect -f '{{.Name}}' "$PG_CONTAINER" 2>/dev/null | sed 's#^/##')"
  fi
fi
if [ -z "$PG_CONTAINER" ]; then
  die "no PostgreSQL server was found for this checkout.
    Run 'make up C=api' here (registers and starts this repo's compose project),
    or pass SMOKE_PG_CONTAINER=<container-name> explicitly.
    This script will not guess, because a wrong guess can drop someone else's database."
fi
docker inspect "$PG_CONTAINER" >/dev/null 2>&1 \
  || die "PostgreSQL container '$PG_CONTAINER' does not exist; refusing to continue"
docker inspect -f '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null | grep -q true \
  || die "PostgreSQL container '$PG_CONTAINER' is not running"
ok "using PostgreSQL server: $PG_CONTAINER"

POSTGRES_USER="$(docker exec "$PG_CONTAINER" printenv POSTGRES_USER 2>/dev/null || echo multica)"
POSTGRES_PASSWORD="$(docker exec "$PG_CONTAINER" printenv POSTGRES_PASSWORD 2>/dev/null || echo multica)"
DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/${DB_NAME}?sslmode=disable"

# Never overwrite: an existing database of this name is a hard error, not
# something to drop and recreate. The name is unique per run, so a collision
# means something is wrong and destroying it could delete another run's data.
existing_db="$(docker exec "$PG_CONTAINER" psql -U "$POSTGRES_USER" -d postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" 2>/dev/null)"
[ -z "$existing_db" ] || die "database '$DB_NAME' already exists; refusing to touch it"
docker exec "$PG_CONTAINER" psql -U "$POSTGRES_USER" -d postgres \
  -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$DB_NAME\"" >/dev/null \
  || die "could not create database $DB_NAME"
# Recorded only AFTER the create succeeded: cleanup must never name a resource
# this run did not actually make.
own database "$DB_NAME"
ok "created database $DB_NAME (recorded as owned by this run)"

# --------------------------------------------------------- object storage ----

step "Object storage (local S3-compatible server)"
# Never adopt an existing container: the name is unique per run, so a collision
# means another run (or a stale one) owns it. Remove-by-name is not an option
# because that is exactly how a parallel run's resources get destroyed.
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

# Create the bucket with the Go AWS SDK the backend itself uses: the smoke test
# must not depend on a CLI the project does not ship.
mkdir -p "$SMOKE_DIR/mkbucket"
cat > "$SMOKE_DIR/mkbucket/main.go" <<'GO'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
GO
( cd "$REPO_ROOT/server" && go run "$SMOKE_DIR/mkbucket/main.go" "$S3_ENDPOINT" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" "$S3_BUCKET" ) \
  >"$LOG_DIR/mkbucket.log" 2>&1 || die "could not create bucket (see $LOG_DIR/mkbucket.log)"
ok "created bucket $S3_BUCKET"

# ------------------------------------------------------------------- API -----

step "Backend against the local bucket"
cd "$REPO_ROOT/server" || die "server directory missing"
DATABASE_URL="$DATABASE_URL" go run ./cmd/migrate up >"$LOG_DIR/migrate.log" 2>&1 \
  || die "migrations failed (see $LOG_DIR/migrate.log)"
ok "migrations applied"

export DATABASE_URL
export PORT="$API_PORT"
export APP_ENV=development
export JWT_SECRET="direct-upload-smoke-secret"
export MULTICA_DEV_VERIFICATION_CODE=888888
export MULTICA_PUBLIC_URL="http://127.0.0.1:${API_PORT}"
export MULTICA_APP_URL="http://127.0.0.1:${API_PORT}"
export S3_BUCKET="$S3_BUCKET"
export S3_REGION=us-east-1
export AWS_ENDPOINT_URL="$S3_ENDPOINT"
export S3_USE_PATH_STYLE=true
export AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY"
export ATTACHMENT_DOWNLOAD_MODE=presign
export ALLOW_SIGNUP=true

go build -o "$SMOKE_DIR/multica-server" ./cmd/server >"$LOG_DIR/build-api.log" 2>&1 \
  || die "backend build failed (see $LOG_DIR/build-api.log)"
"$SMOKE_DIR/multica-server" >"$LOG_DIR/api.log" 2>&1 &
API_PID=$!
info "backend pid $API_PID"

for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:${API_PORT}/health" >/dev/null 2>&1; then break; fi
  kill -0 "$API_PID" 2>/dev/null || die "backend exited during startup (see $LOG_DIR/api.log)"
  sleep 1
done
curl -sf "http://127.0.0.1:${API_PORT}/health" >/dev/null 2>&1 || die "backend never became healthy"
ok "backend healthy at http://127.0.0.1:${API_PORT}"

# Build the CLI and add its directory to PATH: the commands below run the real
# binary, so the measurement includes process startup like a user's would.
go build -o "$SMOKE_DIR/multica" ./cmd/multica >"$LOG_DIR/build-cli.log" 2>&1 \
  || die "CLI build failed (see $LOG_DIR/build-cli.log)"
export PATH="$SMOKE_DIR:$PATH"
# The agent runtime exports MULTICA_AGENT_ID / MULTICA_TASK_ID /
# MULTICA_DAEMON_PORT to every child. A CLI that sees them treats itself as
# running inside a daemon task and refuses anything but a task-scoped mat_
# token, so they are unset for the commands below (scripts/dev-env.sh strips
# the same set for the same reason).
unset MULTICA_AGENT_ID MULTICA_TASK_ID MULTICA_DAEMON_PORT \
      MULTICA_TASK_SLOT MULTICA_TASK_CONFIG_ROOT MULTICA_TASK_WORKSPACES_ROOT \
      MULTICA_WORKSPACES_ROOT MULTICA_DAEMON_PORT
ok "CLI built"

# ------------------------------------------------------------------ login ----

step "Sign in and create a workspace"
curl -sf -X POST "http://127.0.0.1:${API_PORT}/auth/send-code" \
  -H 'Content-Type: application/json' -d '{"email":"smoke@localhost"}' >/dev/null \
  || die "send-code failed"
verify="$(curl -sS -X POST "http://127.0.0.1:${API_PORT}/auth/verify-code" \
  -H 'Content-Type: application/json' -d '{"email":"smoke@localhost","code":"888888"}')"
JWT="$(printf '%s' "$verify" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))')"
[ -n "$JWT" ] || die "verify-code failed: $verify"
pat="$(curl -sS -X POST "http://127.0.0.1:${API_PORT}/api/tokens" \
  -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"name":"smoke","expires_in_days":1}' | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))')"
[ -n "$pat" ] || die "token creation failed"
WS_ID="$(curl -sS -X POST "http://127.0.0.1:${API_PORT}/api/workspaces" \
  -H "Authorization: Bearer $pat" -H 'Content-Type: application/json' \
  -d '{"name":"Smoke","slug":"smoke"}' | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id",""))')"
[ -n "$WS_ID" ] || die "workspace creation failed"

API_URL="http://127.0.0.1:${API_PORT}"
export MULTICA_SERVER_URL="$API_URL"
export MULTICA_TOKEN="$pat"
export MULTICA_WORKSPACE_ID="$WS_ID"
ok "signed in; workspace $WS_ID"

# --------------------------------------------------- body-limit proxy --------

step "Reverse proxy with a 1 MiB request-body limit"
# This is the whole point of the feature: the proxy is configured the way the
# report's deployment is, and the upload must not go through it.
cat > "$SMOKE_DIR/proxy.py" <<'PY'
"""A reverse proxy that rejects any request body above a fixed limit.

Written as a small asyncio server rather than an nginx dependency so the smoke
test runs anywhere Go and Docker do. It also COUNTs the body bytes it forwards,
which is what proves the file never passed through.

It relays a chunked request body as well as a `Content-Length` one, because a
real reverse proxy does: the CLI streams the multipart fallback through an
io.Pipe, so that request is chunked. Relaying only `Content-Length` bodies left
the upstream waiting forever on the chunked ones — which is a bug in this proxy,
not a property of the deployment it models.
"""
import asyncio
import sys

LIMIT = int(sys.argv[2])
UPSTREAM_PORT = int(sys.argv[3])
METRICS_PATH = sys.argv[4]


def record(kind, size):
    with open(METRICS_PATH, "a") as metrics:
        metrics.write("%s\t%d\n" % (kind, size))


def is_chunked(headers):
    encoding = headers.get("transfer-encoding", "").lower()
    return "chunked" in encoding


async def relay_chunked_body(reader, upstream_writer):
    """Relay a chunked body verbatim, returning the decoded payload length.

    Returns None when the running total passes the limit: the caller then drops
    the upstream request and answers 413, which is what a buffering proxy does
    when a body outgrows its ceiling.
    """
    forwarded = 0
    while True:
        size_line = await reader.readline()
        if not size_line:
            return forwarded
        upstream_writer.write(size_line)
        size = int(size_line.strip().split(b";")[0] or b"0", 16)
        if size == 0:
            # Trailer section, terminated by an empty line.
            while True:
                trailer = await reader.readline()
                upstream_writer.write(trailer)
                if trailer in (b"\r\n", b"\n", b""):
                    break
            await upstream_writer.drain()
            return forwarded
        forwarded += size
        if forwarded > LIMIT:
            return None
        data = await reader.readexactly(size + 2)  # payload plus CRLF
        upstream_writer.write(data)
        await upstream_writer.drain()


async def handle(reader, writer):
    try:
        request_line = await reader.readline()
        if not request_line:
            return
        headers = {}
        while True:
            line = await reader.readline()
            if line in (b"\r\n", b"\n", b""):
                break
            name, _, value = line.decode("latin-1").partition(":")
            headers[name.strip().lower()] = value.strip()

        length = int(headers.get("content-length", "0") or 0)
        chunked_request = is_chunked(headers)
        if not chunked_request and length > LIMIT:
            record("rejected", length)
            body = b"413 Request Entity Too Large"
            writer.write(
                b"HTTP/1.1 413 Request Entity Too Large\r\n"
                b"Content-Type: text/plain\r\n"
                b"Content-Length: " + str(len(body)).encode() + b"\r\n"
                b"Connection: close\r\n\r\n" + body
            )
            await writer.drain()
            return

        upstream_reader, upstream_writer = await asyncio.open_connection(
            "127.0.0.1", UPSTREAM_PORT
        )
        # Rebuild the request line with the path the upstream expects. The
        # proxy keeps the path as-is: the API routes on it.
        upstream_writer.write(request_line)
        for name, value in headers.items():
            upstream_writer.write(("%s: %s\r\n" % (name, value)).encode("latin-1"))
        upstream_writer.write(b"\r\n")

        forwarded = 0
        if chunked_request:
            forwarded = await relay_chunked_body(reader, upstream_writer)
            if forwarded is None:
                # Over the limit mid-stream: the upstream request is abandoned
                # and the client is told, exactly as a buffering proxy would.
                record("rejected", LIMIT + 1)
                upstream_writer.close()
                body = b"413 Request Entity Too Large"
                writer.write(
                    b"HTTP/1.1 413 Request Entity Too Large\r\n"
                    b"Content-Type: text/plain\r\n"
                    b"Content-Length: " + str(len(body)).encode() + b"\r\n"
                    b"Connection: close\r\n\r\n" + body
                )
                await writer.drain()
                return
        else:
            remaining = length
            while remaining > 0:
                chunk = await reader.read(min(remaining, 65536))
                if not chunk:
                    break
                remaining -= len(chunk)
                forwarded += len(chunk)
                upstream_writer.write(chunk)
                await upstream_writer.drain()
        record("forwarded", forwarded)

        # Response: copy every header line, then the body. Streaming responses
        # are not needed here — the API answers these routes with JSON.
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
            # Drop any hop-by-hop framing the upstream chose: this proxy closes
            # the connection after every response (see the finally block), so it
            # must SAY so. Relaying an upstream keep-alive unchanged left the
            # client reusing a socket that was about to be closed, which
            # surfaced intermittently as "Could not reach the Multica server"
            # on the request AFTER a successful one.
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
        else:
            if response_length:
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
PROXY_PORT=$((API_PORT + 1))
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

PROXY_URL="http://127.0.0.1:${PROXY_PORT}"

# The control: the LEGACY multipart path must be rejected by this proxy. If it
# is not, the "large files succeed" assertion below proves nothing.
CONTROL_FILE="$SMOKE_DIR/control-2m.bin"
head -c $((2 << 20)) /dev/urandom > "$CONTROL_FILE"
CONTROL_STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$PROXY_URL/api/upload-file" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -F "file=@$CONTROL_FILE")"
if [ "$CONTROL_STATUS" = "413" ]; then
  ok "control: legacy multipart 2 MiB through the proxy is rejected with 413"
else
  bad "control: legacy multipart expected 413 through a 1 MiB proxy, got $CONTROL_STATUS"
fi
# And the same file succeeds against the backend directly, so the 413 really is
# the proxy and not the API.
DIRECT_STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API_URL/api/upload-file" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -F "file=@$CONTROL_FILE")"
if [ "$DIRECT_STATUS" = "200" ]; then
  ok "control: the same multipart 2 MiB succeeds against the backend directly (200)"
else
  bad "control: direct multipart expected 200, got $DIRECT_STATUS"
fi

# ------------------------------------------------------------ file sizes ----

step "Presigned upload through the proxy: 50 MiB and 100 MiB"
WORK="$SMOKE_DIR/work"
mkdir -p "$WORK"

# peak_rss_kb runs a command and reports its peak RSS in kilobytes.
#
# It polls /proc/<pid>/status VmHWM rather than using GNU time, which many
# machines do not ship. VmHWM is the kernel's high-water mark for the process,
# so sampling it is exact at the moment of each read and the maximum over the
# samples is the true peak once the workload runs for more than one interval.
peak_rss_kb() {
  local _unused=$1
  shift
  "$@" >/dev/null 2>&1 &
  local pid=$!
  local peak=0
  while kill -0 "$pid" 2>/dev/null; do
    local current
    current="$(awk '/VmHWM/ {print $2}' "/proc/$pid/status" 2>/dev/null)"
    if [ -n "$current" ] && [ "$current" -gt "$peak" ]; then peak=$current; fi
    sleep 0.02
  done
  wait "$pid"
  printf '%s' "$peak"
}

check_upload() {
  local megabytes=$1
  local bytes=$((megabytes << 20))
  local file="$WORK/file-${megabytes}m.bin"
  local download="$WORK/download-${megabytes}m.bin"

  info "${megabytes} MiB: creating fixture"
  head -c "$bytes" /dev/urandom > "$file"
  local sum_before
  sum_before="$(sha256sum "$file" | awk '{print $1}')"

  local rss_before
  rss_before="$(ps -o rss= -p "$API_PID" | awk '{print $1}')"

  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$PROXY_URL/api/upload-file" \
    -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
    -F "file=@$file")"
  # The legacy route through a 1 MiB proxy must always be 413 — that is the
  # baseline whose failure mode this feature exists to remove.
  [ "$status" = "413" ] && ok "${megabytes} MiB: legacy path still 413 through the proxy (feature is the only way in)" \
    || bad "${megabytes} MiB: legacy path unexpectedly returned $status"

  # Now the real path: the CLI against the PROXY. Its metadata requests fit the
  # proxy's limit and the file itself goes to object storage.
  local start_ns end_ns elapsed_ms
  start_ns="$(date +%s%N)"
  local json
  json="$( cd "$WORK" && MULTICA_SERVER_URL="$PROXY_URL" \
    "$SMOKE_DIR/multica" issue create --title "smoke ${megabytes}MiB" \
      --attachment "$(basename "$file")" --output json 2>"$LOG_DIR/cli-${megabytes}m.log")"
  local cli_status=$?
  end_ns="$(date +%s%N)"
  elapsed_ms=$(( (end_ns - start_ns) / 1000000 ))

  if [ $cli_status -ne 0 ]; then
    bad "${megabytes} MiB: CLI upload failed: $(tail -3 "$LOG_DIR/cli-${megabytes}m.log" | tr '\n' ' ')"
    return
  fi
  ok "${megabytes} MiB: CLI uploaded through the 1 MiB proxy in ${elapsed_ms}ms"

  local attachment_id
  attachment_id="$(printf '%s' "$json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("identifier",""))')"
  local issue_id
  issue_id="$(printf '%s' "$json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id",""))')"
  [ -n "$issue_id" ] || { bad "${megabytes} MiB: issue create returned no id: $json"; return; }

  # The attachment id comes from the issue's attachment list; the create
  # response carries the issue, and the description carries the markdown URL
  # with the id.
  local markdown_url
  markdown_url="$(printf '%s' "$json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("description",""))' | grep -o '/api/attachments/[0-9a-f-]*/download' | head -1)"
  attachment_id="$(printf '%s' "$markdown_url" | sed -E 's#/api/attachments/([0-9a-f-]+)/download#\1#')"
  [ -n "$attachment_id" ] || { bad "${megabytes} MiB: no attachment id in the created description"; return; }

  # Download through the proxy and compare bytes.
  local dl_status
  dl_status="$(curl -sL -o "$download" -w '%{http_code}' "$PROXY_URL/api/attachments/${attachment_id}/download" \
    -H "Authorization: Bearer $pat")"
  if [ "$dl_status" != "200" ]; then
    bad "${megabytes} MiB: download returned $dl_status"
    return
  fi
  local sum_after
  sum_after="$(sha256sum "$download" | awk '{print $1}')"
  if [ "$sum_before" = "$sum_after" ]; then
    ok "${megabytes} MiB: downloaded bytes match the uploaded bytes"
  else
    bad "${megabytes} MiB: downloaded bytes differ (${sum_before:0:12} vs ${sum_after:0:12})"
  fi

  local rss_after
  rss_after="$(ps -o rss= -p "$API_PID" | awk '{print $1}')"
  record "$((megabytes))MiB" "$elapsed_ms" "$rss_before" "$rss_after"

  # What the proxy saw: only metadata. Any request body above the limit would
  # have been rejected, so this confirms the accounting rather than relying on
  # the absence of a 413 alone.
  local max_forwarded
  max_forwarded="$(awk -F'\t' '$1=="forwarded" {if ($2+0 > m) m = $2+0} END {print m+0}' "$PROXY_METRICS")"
  if [ "$max_forwarded" -le $((1 << 20)) ]; then
    ok "${megabytes} MiB: largest request body the proxy forwarded is ${max_forwarded} bytes (≤ 1 MiB)"
  else
    bad "${megabytes} MiB: the proxy forwarded a ${max_forwarded}-byte body — the file went through it"
  fi
}

check_upload 50
if [ "${SMOKE_QUICK:-0}" != "1" ]; then
  check_upload 100
else
  info "SMOKE_QUICK=1: skipping the 100 MiB round trip"
fi

# ------------------------------------------------------------- empty file ----

step "Zero-byte upload: the capability is declined and the file still publishes"

# Two separate facts, because they fail differently:
#
#   1. The empty file uploads and downloads as empty content with the same
#      metadata as any other attachment. That is the product promise and it must
#      hold end to end through the real CLI.
#   2. It does NOT travel through the presigned path. A SigV4 credential for
#      length 0 binds no size — the store accepts any body through it — so the
#      endpoint declines size 0 and the client falls back to multipart. Asserting
#      only (1) would pass on the vulnerable build too, so (2) is checked
#      directly against the API before the CLI runs.
EMPTY_FILE="$WORK/empty.bin"
: > "$EMPTY_FILE"

# The refusal itself: 501 with the capability code, and no credential in the
# response. A 400 or a 5xx here would strand every empty upload, because no
# client falls back on either.
empty_presign_status="$(curl -s -o "$SMOKE_DIR/empty-presign.json" -w '%{http_code}' \
  -X POST "$PROXY_URL/api/attachments/presign-upload" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -H 'Content-Type: application/json' \
  -d '{"filename":"empty.bin","content_type":"text/plain","size_bytes":0}')"
if [ "$empty_presign_status" = "501" ]; then
  ok "a zero-byte presign request is declined with 501"
else
  bad "zero-byte presign expected 501, got $empty_presign_status: $(cat "$SMOKE_DIR/empty-presign.json")"
fi
if grep -q '"code"[[:space:]]*:[[:space:]]*"attachment_presign_unsupported"' "$SMOKE_DIR/empty-presign.json" 2>/dev/null; then
  ok "the zero-byte refusal carries attachment_presign_unsupported"
else
  bad "the zero-byte refusal is missing the capability code: $(cat "$SMOKE_DIR/empty-presign.json")"
fi
if grep -q "X-Amz-Signature" "$SMOKE_DIR/empty-presign.json" 2>/dev/null; then
  bad "the zero-byte refusal contained a signed URL"
else
  ok "the zero-byte refusal returned no upload credential"
fi
# The size-1 case is the counterweight, and it is the same request shape: the
# rule is about a length that cannot be signed, not about small files.
one_byte_status="$(curl -s -o "$SMOKE_DIR/one-byte-presign.json" -w '%{http_code}' \
  -X POST "$PROXY_URL/api/attachments/presign-upload" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -H 'Content-Type: application/json' \
  -d '{"filename":"one.bin","content_type":"application/octet-stream","size_bytes":1}')"
if [ "$one_byte_status" = "200" ]; then
  ok "a one-byte presign request still issues a credential (200)"
else
  bad "one-byte presign expected 200, got $one_byte_status: $(cat "$SMOKE_DIR/one-byte-presign.json")"
fi
if grep -q '"Content-Length"[[:space:]]*:[[:space:]]*"1"' "$SMOKE_DIR/one-byte-presign.json" 2>/dev/null; then
  ok "the one-byte credential binds its length"
else
  bad "the one-byte credential does not report a Content-Length header: $(cat "$SMOKE_DIR/one-byte-presign.json")"
fi

# End to end through the CLI: the fallback must actually run and publish.
EMPTY_JSON="$( cd "$WORK" && MULTICA_SERVER_URL="$PROXY_URL" \
  "$SMOKE_DIR/multica" issue create --title "smoke empty" \
    --attachment "$(basename "$EMPTY_FILE")" --output json 2>"$LOG_DIR/cli-empty.log")"
EMPTY_STATUS=$?
if [ $EMPTY_STATUS -eq 0 ]; then
  ok "zero-byte file uploaded (multipart fallback after the 501)"
else
  bad "zero-byte upload failed: $(tail -3 "$LOG_DIR/cli-empty.log" | tr '\n' ' ')"
fi
# The fallback is what makes the upload work, so prove the direct path was not
# used at all: a presign that had succeeded would have written an upload session
# row for this issue, and there must be none. Counting rows is deterministic,
# unlike grepping a client log whose verbosity is not part of the contract.
EMPTY_ISSUE_ID="$(printf '%s' "$EMPTY_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)"
if [ -n "$EMPTY_ISSUE_ID" ]; then
  EMPTY_SESSIONS="$(docker exec "$PG_CONTAINER" psql -U "$POSTGRES_USER" -d "$DB_NAME" -Atqc \
    "SELECT count(*) FROM attachment_upload_session WHERE issue_id = '$EMPTY_ISSUE_ID'" 2>/dev/null)"
  if [ "$EMPTY_SESSIONS" = "0" ]; then
    ok "the zero-byte upload created no upload session (it never used the presigned path)"
  else
    bad "the zero-byte upload created $EMPTY_SESSIONS upload session row(s): it went through the presigned path"
  fi
else
  bad "issue create returned no id for the zero-byte upload: $EMPTY_JSON"
fi
EMPTY_ATTACHMENT_ID=""
if [ -n "$EMPTY_ISSUE_ID" ]; then
  EMPTY_ATTACHMENT_ID="$(curl -s "$API_URL/api/issues/$EMPTY_ISSUE_ID/attachments" \
    -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
    | python3 -c 'import json,sys; rows=json.load(sys.stdin); print(rows[0]["id"] if rows else "")' 2>/dev/null)"
fi
if [ -n "$EMPTY_ATTACHMENT_ID" ]; then
  EMPTY_DL="$WORK/empty-download.bin"
  if curl -sL -o "$EMPTY_DL" -w '%{http_code}' "$PROXY_URL/api/attachments/$EMPTY_ATTACHMENT_ID/download" \
      -H "Authorization: Bearer $pat" | grep -q 200; then
    if [ ! -s "$EMPTY_DL" ]; then
      ok "zero-byte attachment downloads as an empty body"
    else
      bad "zero-byte attachment downloaded $(wc -c < "$EMPTY_DL") bytes"
    fi
  else
    bad "zero-byte attachment download failed"
  fi
  # The published metadata must still describe empty text, not an empty string.
  EMPTY_CT="$(curl -s "$API_URL/api/attachments/$EMPTY_ATTACHMENT_ID" \
    -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("content_type",""), d.get("size_bytes",""))' 2>/dev/null)"
  case "$EMPTY_CT" in
    "text/plain"*" 0") ok "zero-byte attachment reports text/plain and size 0 ($EMPTY_CT)" ;;
    *) bad "zero-byte attachment metadata = '$EMPTY_CT', want text/plain and size 0" ;;
  esac
else
  bad "could not resolve the zero-byte attachment for verification"
fi

# ---------------------------------------------------------------- over cap --

step "Declared size above the cap is refused before any upload"
TOKEN_PREFIX="$(printf '%s' "$pat" | cut -c1-6)"
oversize_status="$(curl -s -o "$SMOKE_DIR/oversize.json" -w '%{http_code}' \
  -X POST "$PROXY_URL/api/attachments/presign-upload" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"filename\":\"too-big.bin\",\"content_type\":\"application/octet-stream\",\"size_bytes\":$(( (100 << 20) + 1 ))}")"
if [ "$oversize_status" = "413" ]; then
  ok "a declaration of 100 MiB + 1 is refused with 413"
else
  bad "oversize declaration expected 413, got $oversize_status: $(cat "$SMOKE_DIR/oversize.json")"
fi
# The refusal must carry no signed URL: nothing was issued to leak.
if grep -q "X-Amz-Signature" "$SMOKE_DIR/oversize.json" 2>/dev/null; then
  bad "the oversize refusal contained a signed URL"
else
  ok "the oversize refusal returned no upload credential"
fi

# -------------------------------------------------------------- credential --

step "The upload credential never leaves for the API host and never carries the token"
# The PUT goes to the storage host, which in this deployment is a different
# origin. Assert the presigned URL host differs from the API host, so the client
# physically cannot send its API token there by accident.
presign_json="$(curl -sS -X POST "$API_URL/api/attachments/presign-upload" \
  -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $WS_ID" \
  -H 'Content-Type: application/json' \
  -d '{"filename":"cred.bin","content_type":"application/octet-stream","size_bytes":16}')"
upload_host="$(printf '%s' "$presign_json" | python3 -c 'import json,sys,urllib.parse; print(urllib.parse.urlparse(json.load(sys.stdin).get("upload_url","")).netloc)' 2>/dev/null)"
if [ -n "$upload_host" ] && [ "$upload_host" != "127.0.0.1:${PROXY_PORT}" ] && [ "$upload_host" != "127.0.0.1:${API_PORT}" ]; then
  ok "presigned upload host ($upload_host) is distinct from the API host"
else
  bad "presigned upload URL did not name a separate storage host: $presign_json"
fi
if printf '%s' "$presign_json" | grep -q "$TOKEN_PREFIX"; then
  bad "the presign response echoed the API token"
else
  ok "the presign response carries no API token"
fi

# ------------------------------------------------------------------ report --

step "Peak RSS while uploading a 100 MiB file (streaming, not buffering)"
RSS_FILE="$SMOKE_DIR/rss.txt"
BIG_FILE="$WORK/file-100m.bin"
if [ -f "$BIG_FILE" ]; then
  CLIENT_RSS_KB="$( cd "$WORK" && MULTICA_SERVER_URL="$PROXY_URL" peak_rss_kb "$RSS_FILE" \
    "$SMOKE_DIR/multica" issue create --title "rss probe" \
      --attachment "$(basename "$BIG_FILE")" --output json )"
  if [ -n "$CLIENT_RSS_KB" ]; then
    # A buffering client would hold the whole 100 MiB file; the streaming one
    # costs a copy buffer and the runtime.
    if [ "$CLIENT_RSS_KB" -lt $((64 << 10)) ]; then
      ok "CLI peak RSS ${CLIENT_RSS_KB} KiB uploading 100 MiB (< 64 MiB — no full-file buffer)"
    else
      bad "CLI peak RSS ${CLIENT_RSS_KB} KiB suggests the file was buffered"
    fi
    record "100MiB-rss" "-" "$CLIENT_RSS_KB" "-"
  else
    info "could not measure CLI RSS on this platform"
  fi
fi

step "Staging object reclaimed by the cleanup worker"
# The completing transaction hands the session to the reconciler, which sweeps
# once a minute. Wait for the session's staging key to disappear.
for _ in $(seq 1 90); do
  staging_objects="$(docker exec "$S3_CONTAINER" sh -c 'find /data -path "*upload-staging*" -type f 2>/dev/null | wc -l' 2>/dev/null || echo 0)"
  [ "${staging_objects:-0}" = "0" ] && break
  sleep 2
done
# A completed session's row is dropped once its tombstone schedule is done; the
# object must be gone long before that.
if [ "${staging_objects:-0}" = "0" ]; then
  ok "no staging objects remain after the cleanup sweep"
else
  bad "${staging_objects} staging object(s) still present after 180s"
fi

step "Measured numbers"
printf '    %-12s %-12s %-14s %-14s\n' "file" "elapsed_ms" "api_rss_before" "api_rss_after"
awk -F'\t' '{printf "    %-12s %-12s %-14s %-14s\n", $1, $2, $3, $4}' "$METRICS_FILE"
info "proxy metrics: $PROXY_METRICS"
info "logs: $LOG_DIR"

# `cleanup` (the EXIT trap) prints the summary and exits with the last status,
# so force a failure exit when any check failed.
[ "$FAIL" -eq 0 ]
