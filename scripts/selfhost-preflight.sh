#!/usr/bin/env bash
# Refuse to start the self-hosted stack when a host port it would publish is
# already taken. Shared by `make selfhost` and `make selfhost-build`.
#
# Why this runs before `docker compose pull`: without it, the first sign of a
# port conflict is Docker's own "Bind for 127.0.0.1:3000 failed: port is already
# allocated", raised only after several hundred megabytes of images have been
# pulled. And a partially failed `up` still ends in scripts/selfhost-wait.sh
# printing "Services are still starting" and exiting 0, so the conflict can even
# look like a successful install. Checking first costs one `docker compose
# config` call and turns both outcomes into one actionable message.
#
# MULTICA_SELFHOST_SKIP_PORT_CHECK=1 bypasses the check. It exists for
# scripts/selfhost-config.test.sh, which drives these recipes with fixed port
# numbers to assert how a port is *resolved*, and so cannot depend on which
# ports the machine running the tests happens to have free.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mode=${1:-official}
case "$mode" in
official)
  compose_files=(-f docker-compose.selfhost.yml)
  make_target="make selfhost"
  ;;
build)
  compose_files=(-f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml)
  make_target="make selfhost-build"
  ;;
*)
  echo "usage: ${BASH_SOURCE[0]} [official|build]" >&2
  exit 2
  ;;
esac

if [ "${MULTICA_SELFHOST_SKIP_PORT_CHECK:-}" = "1" ]; then
  exit 0
fi

# The Makefile exports COMPOSE; keep the same default when run standalone.
read -r -a compose_cmd <<<"${COMPOSE:-docker compose}"

# Which ports to check comes from Compose, never from re-deriving
# ${BACKEND_PORT:-${API_PORT:-${SERVER_PORT:-${PORT:-8080}}}} a third time here.
# Two sources of truth for the published port is #6145, and the alias chain is
# only half of it: which source wins (env file, environment, make command line)
# differs per entry point. Compose is the only authority on what it will
# publish, so ask it. scripts/selfhost-wait.sh carries the same rule for the
# health probe.
#
# docker-compose.selfhost.yml requires JWT_SECRET (${JWT_SECRET:?...}) and this
# runs before the recipe has created .env, so `config` gets a throwaway value
# purely so the file interpolates. Nothing is started with it.
#
# A `config` that fails for any other reason prints nothing, which reports no
# conflicts and lets the real `pull`/`up` surface the error with its own
# message: this check only ever adds a failure it can explain.
compose_published_ports() {
  JWT_SECRET="${JWT_SECRET:-selfhost-preflight}" \
    "${compose_cmd[@]}" "${compose_files[@]}" config 2>/dev/null | awk '
    /^[a-zA-Z0-9_-]+:/ { in_services = ($0 == "services:"); next }
    !in_services { next }
    /^  [a-zA-Z0-9_.-]+:$/ { service = substr($1, 1, length($1) - 1); next }
    /^        target: / { target = $2; next }
    /^        published: / {
      published = $2
      gsub(/"/, "", published)
      print service, target, published
    }
  '
}

# A connect probe on 127.0.0.1 rather than `lsof`, because that is the question
# being asked: docker-compose.selfhost.yml publishes on 127.0.0.1 only, so a
# listener bound to another interface (192.168.1.10:8080, say) does not collide
# and must not be reported as a conflict. port_free() in scripts/dev-env.sh uses
# `lsof -sTCP:LISTEN`, which lists listeners on every interface — right for that
# script, wrong here — and reports "free" on a host with no lsof installed,
# which is the common case on a small self-host server. bash's /dev/tcp is
# always present, sees listeners on both 127.0.0.1 and 0.0.0.0, and misses
# exactly the ones that genuinely do not conflict.
port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

# Best-effort name for the offender, in the spirit of describe_port_owner() in
# scripts/dev-env.sh. Decoration only: when nothing can name the holder there is
# no name, and the verdict is unchanged.
describe_port_owner() {
  local port=$1 container pid comm

  # Ask Docker first. On a self-host box the holder is usually another
  # container, and that is the case lsof describes worst: the listening socket
  # belongs to root's docker-proxy, so a non-root lsof sees nothing at all and a
  # root one reports "docker-proxy", which names no container.
  container=$(docker ps --filter "publish=${port}" --format '{{.Names}}' 2>/dev/null | head -1 || true)
  if [ -n "$container" ]; then
    printf ' by the Docker container %s' "$container"
    return 0
  fi

  command -v lsof >/dev/null 2>&1 || return 0
  pid=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -1 || true)
  [ -n "$pid" ] || return 0

  comm=$(ps -p "$pid" -o comm= 2>/dev/null | tr -d '[:space:]' || true)
  if [ -n "$comm" ]; then
    printf ' by %s (pid %s)' "$comm" "$pid"
  else
    printf ' by pid %s' "$pid"
  fi
}

# A port this project's own container already publishes is not a conflict:
# re-running `make selfhost` is how the stack picks up a new image, and it has
# to stay idempotent. `docker compose port` answers for the running container
# only, which is the same primitive scripts/selfhost-wait.sh uses.
already_published_by_us() {
  local service=$1 container_port=$2 port=$3 published

  published=$(
    JWT_SECRET="${JWT_SECRET:-selfhost-preflight}" \
      "${compose_cmd[@]}" "${compose_files[@]}" port "$service" "$container_port" 2>/dev/null | tail -n 1
  ) || published=""
  published=${published##*:}
  published=${published%$'\r'}

  [ -n "$published" ] && [ "$published" = "$port" ]
}

# The variable an operator sets to move each service. Keep this in step with
# docker-compose.selfhost.yml: the backend reads the PORT alias chain, and PORT
# is the documented one to edit.
port_variable() {
  case "$1" in
  frontend) printf 'FRONTEND_PORT' ;;
  *) printf 'PORT' ;;
  esac
}

# Plain strings, not arrays: `${#array[@]}` on an empty array is an unbound
# variable error under `set -u` in bash 3.2, which is what macOS still ships.
conflict_count=0
conflict_lines=""
suggestion=""

while read -r service target published; do
  [ -n "${published:-}" ] || continue
  port_in_use "$published" || continue
  if already_published_by_us "$service" "$target" "$published"; then
    continue
  fi

  conflict_count=$((conflict_count + 1))
  conflict_lines="${conflict_lines}  Port ${published} (${service}) is already in use$(describe_port_owner "$published").
"
  suggestion="${suggestion} $(port_variable "$service")=<free port>"
done < <(compose_published_ports)

if [ "$conflict_count" -eq 0 ]; then
  exit 0
fi

echo "" >&2
printf '%s' "$conflict_lines" >&2
echo "" >&2
echo "Multica was not started; no images were pulled." >&2
echo "" >&2
echo "Pick free ports and run again:" >&2
echo "  ${make_target}${suggestion}" >&2
echo "" >&2
if [ -f .env ]; then
  # The recipe only writes ports into a .env it creates, so an existing file
  # keeps whatever it already says and a command-line port would last one run.
  echo "Set the same values in .env to keep them." >&2
else
  echo "They are written to the .env that run creates, so later runs keep them." >&2
fi
exit 1
