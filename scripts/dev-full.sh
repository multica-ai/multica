#!/usr/bin/env bash
# Fresh-machine bootstrap: starts the WeKnora knowledge services, fills in the
# knowledge secrets that are still empty, then runs `make dev` (migrations, API,
# web). Re-running is safe: values already in the env file are kept, and
# containers that already exist are not stopped.
#
# Steps that need software or accounts outside this repo (the model gateway,
# Hermes, the daemon) are described in docs/engineering/runbooks/fresh-machine-install.md.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

GUIDE="docs/engineering/runbooks/fresh-machine-install.md"
COMPOSE_FILE="docker-compose.selfhost.yml"
KNOWLEDGE_SERVICES=(knowledge-postgres knowledge-redis knowledge-docreader knowledge-app knowledge-ui)
REQUIRED_KNOWLEDGE=(
  KNOWLEDGE_SERVICE_EMAIL
  KNOWLEDGE_SERVICE_PASSWORD
  KNOWLEDGE_LLM_BASE_URL
  KNOWLEDGE_LLM_API_KEY
  KNOWLEDGE_LLM_MODEL
  KNOWLEDGE_EMBEDDING_URL
  KNOWLEDGE_EMBEDDING_MODEL
)

info() { echo "==> $*"; }
ok() { echo "✓ $*"; }
fail() { echo "✗ $*" >&2; exit 1; }

# ---------- Prerequisites (same checks as scripts/dev.sh, plus compose) ----------
check_prerequisites() {
  local missing=() tool
  for tool in node pnpm go docker openssl curl; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  if [ ${#missing[@]} -gt 0 ]; then
    fail "Missing prerequisites: ${missing[*]}
  Please install: Node.js 22, pnpm 10.28.2, Go 1.26.9, Docker with the compose plugin
  Install steps: $GUIDE#prerequisites"
  fi
  docker compose version >/dev/null 2>&1 \
    || fail "Docker Compose ('docker compose') was not found. Install the compose plugin: $GUIDE#prerequisites"
  docker info >/dev/null 2>&1 || fail "The Docker daemon is not running. Start Docker and run make dev-full again."
  ok "Prerequisites found"

  if command -v hermes >/dev/null 2>&1; then
    HERMES_STATUS="on PATH; the daemon registers it when the daemon starts"
  else
    HERMES_STATUS="not registered (hermes is not on PATH)"
    echo "! Hermes CLI not found on PATH. Continuing without it. Install steps: $GUIDE#hermes"
  fi
}

# ---------- Env file: same resolution as scripts/dev.sh ----------
resolve_env_file() {
  if [ -f .git ]; then
    # A linked worktree. init-worktree-env.sh gives it its own database and ports;
    # copying .env.example would reuse the main checkout's database.
    ENV_FILE=".env.worktree"
    if [ ! -f "$ENV_FILE" ]; then
      info "Worktree detected. Generating $ENV_FILE..."
      bash scripts/init-worktree-env.sh "$ENV_FILE"
    fi
  else
    ENV_FILE=".env"
    if [ ! -f "$ENV_FILE" ]; then
      info "Creating $ENV_FILE from .env.example..."
      cp .env.example "$ENV_FILE"
    fi
  fi
  ok "Using $ENV_FILE"
}

sed_in_place() {
  if [ "$(uname)" = "Darwin" ]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

# Value of the last uncommented KEY= line in a file; empty when unset or commented out.
env_value() {
  local line
  line=$(grep -E "^$2=" "$1" | tail -n 1 || true)
  printf '%s' "${line#"$2="}"
}

# Sets KEY to VALUE only when KEY is empty or absent. Values are never printed.
set_if_empty() {
  local key=$1 value=$2
  if [ -n "$(env_value "$ENV_FILE" "$key")" ]; then
    return 0
  fi
  if grep -qE "^${key}=" "$ENV_FILE"; then
    sed_in_place "s#^${key}=.*#${key}=${value}#" "$ENV_FILE"
  else
    if [ -s "$ENV_FILE" ] && [ -n "$(tail -c 1 "$ENV_FILE")" ]; then
      printf '\n' >> "$ENV_FILE"
    fi
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
  ok "Set $key in $ENV_FILE"
}

# Every compose call goes through here. Compose refuses to parse the file unless the
# backend's JWT_SECRET is set (a `:?` check), even though the backend never starts
# here. When the env file has no JWT_SECRET, a throwaway value is passed to this one
# compose process; it is not written to the env file, because Multica's own secret
# must not change on a machine that already runs.
compose() {
  local jwt
  jwt=$(env_value "$ENV_FILE" JWT_SECRET)
  JWT_SECRET="${jwt:-$(openssl rand -hex 32)}" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" --profile knowledge "$@"
}

knowledge_postgres_exists() {
  [ -n "$(compose ps -a -q knowledge-postgres 2>/dev/null)" ]
}

# ---------- Knowledge secrets: generated only where the env file is empty ----------
fill_knowledge_secrets() {
  set_if_empty KNOWLEDGE_SYSTEM_AES_KEY "$(openssl rand -hex 16)"
  set_if_empty KNOWLEDGE_JWT_SECRET "$(openssl rand -hex 32)"
  set_if_empty MULTICA_KNOWLEDGE_SECRET_KEY "$(openssl rand -base64 32)"
  set_if_empty KNOWLEDGE_REDIS_PASSWORD "$(openssl rand -hex 16)"
  # Postgres reads its password only when its data volume is first created. A new
  # value would lock the existing knowledge-app out, so an existing container keeps
  # the password it was created with (the compose default unless it was changed).
  if [ -z "$(env_value "$ENV_FILE" KNOWLEDGE_DB_PASSWORD)" ] && knowledge_postgres_exists; then
    echo "! knowledge-postgres already exists, so its password cannot change. KNOWLEDGE_DB_PASSWORD stays empty and compose uses its default (weknora). If you chose another password when the volume was created, set it in $ENV_FILE."
  else
    set_if_empty KNOWLEDGE_DB_PASSWORD "$(openssl rand -hex 16)"
  fi
}

# ---------- Values this script cannot invent ----------
explain_knowledge_key() {
  case "$1" in
    KNOWLEDGE_SERVICE_EMAIL) echo "WeKnora login that Multica signs in as to create one WeKnora workspace per Multica workspace" ;;
    KNOWLEDGE_SERVICE_PASSWORD) echo "Password for that WeKnora login (8 to 32 letters and digits)" ;;
    KNOWLEDGE_LLM_BASE_URL) echo "OpenAI-compatible chat endpoint for summaries, Wiki and reranking, such as the 9router gateway" ;;
    KNOWLEDGE_LLM_API_KEY) echo "Key for the chat endpoint" ;;
    KNOWLEDGE_LLM_MODEL) echo "Chat model name that the endpoint serves" ;;
    KNOWLEDGE_EMBEDDING_URL) echo "Embedding endpoint WeKnora calls; Ollama on the Docker host is http://host.docker.internal:11434" ;;
    KNOWLEDGE_EMBEDDING_MODEL) echo "Embedding model, fixed for each knowledge base (for example bge-m3)" ;;
  esac
}

check_required_knowledge() {
  local key missing=()
  for key in "${REQUIRED_KNOWLEDGE[@]}"; do
    if [ -z "$(env_value "$ENV_FILE" "$key")" ]; then
      missing+=("$key")
    fi
  done
  if [ ${#missing[@]} -eq 0 ]; then
    return 0
  fi
  echo "✗ Nothing was started. These values in $ENV_FILE are empty:" >&2
  for key in "${missing[@]}"; do
    echo "    $key" >&2
    echo "        $(explain_knowledge_key "$key")" >&2
  done
  echo "  Fill them in and run make dev-full again." >&2
  echo "  Variables: $GUIDE#environment-variables" >&2
  echo "  Gateway and endpoints: $GUIDE#the-9router-gateway" >&2
  exit 1
}

# Derives the URLs the host-run backend and the summary use. KNOWLEDGE_URL must be
# the published port: make dev runs on this machine, where the compose service name
# does not resolve.
resolve_knowledge_urls() {
  local api_port ui_port url
  api_port=$(env_value "$ENV_FILE" KNOWLEDGE_API_PORT)
  ui_port=$(env_value "$ENV_FILE" KNOWLEDGE_UI_PORT)
  set_if_empty KNOWLEDGE_URL "http://localhost:${api_port:-8090}"
  set_if_empty KNOWLEDGE_UI_URL "http://localhost:${ui_port:-8085}"
  url=$(env_value "$ENV_FILE" KNOWLEDGE_URL)
  case "$url" in
    *knowledge-app*)
      fail "KNOWLEDGE_URL=$url uses the Docker network name. make dev runs Multica on this machine, so set KNOWLEDGE_URL=http://localhost:${api_port:-8090} in $ENV_FILE and run make dev-full again."
      ;;
  esac
}

load_env() {
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
  # shellcheck disable=SC1091
  . scripts/local-env.sh
}

# Polls a URL until it answers. Returns 1 on timeout, or when the process given as
# the optional third argument has exited.
wait_for_url() {
  local label=$1 url=$2 pid=${3:-} tries=0
  until curl -fsS --max-time 3 -o /dev/null "$url" 2>/dev/null; do
    if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
      echo "! $label: the process stopped before $url answered" >&2
      return 1
    fi
    tries=$((tries + 1))
    if [ "$tries" -ge 120 ]; then
      echo "! $label: $url did not answer after about 6 minutes" >&2
      return 1
    fi
    sleep 3
  done
  ok "$label answers at $url"
}

start_knowledge() {
  local url
  url=$(env_value "$ENV_FILE" KNOWLEDGE_URL)
  info "Starting WeKnora: ${KNOWLEDGE_SERVICES[*]}"
  info "The first start pulls the pinned WeKnora images, which can take several minutes."
  compose up -d "${KNOWLEDGE_SERVICES[@]}"
  wait_for_url "WeKnora" "$url/health" || fail "WeKnora is not healthy. Logs: $(compose_cli_hint) logs --tail 100 knowledge-app. Troubleshooting: $GUIDE#troubleshooting"
}

# The command a person types by hand. Compose needs JWT_SECRET to parse the file; the
# value is unused for the knowledge services, so any placeholder works when the env
# file leaves it empty.
compose_cli_hint() {
  echo "JWT_SECRET=unused docker compose --env-file $ENV_FILE -f $COMPOSE_FILE --profile knowledge"
}

print_summary() {
  echo ""
  echo "Multica and WeKnora are up."
  echo ""
  echo "  Web app       http://localhost:${FRONTEND_PORT}"
  echo "  Multica API   http://localhost:${PORT}  (health: /health)"
  echo "  WeKnora API   ${KNOWLEDGE_URL}  (health: /health)"
  echo "  WeKnora UI    ${KNOWLEDGE_UI_URL}"
  echo ""
  echo "  Running       Multica API, Multica web, WeKnora (${KNOWLEDGE_SERVICES[*]})"
  echo "  Hermes        $HERMES_STATUS"
  echo "  Daemon        not started by this target; run make daemon after the CLI login"
  echo ""
  echo "Next steps in the web app. Full steps: $GUIDE#first-run-in-the-ui"
  echo "  1. Create the account and the first workspace."
  echo "  2. Knowledge: enable knowledge, then check Setup > Models."
  echo "  3. Office: Set up your company, then Build your company."
  echo "  4. Office: check the Secretary's daily brief."
  echo ""
  echo "Ctrl-C stops Multica. WeKnora keeps running. To stop WeKnora:"
  echo "  $(compose_cli_hint) stop ${KNOWLEDGE_SERVICES[*]}"
  echo ""
}

start_multica() {
  info "Starting Multica: make dev (migrations, API, web)"
  make dev &
  DEV_PID=$!
  trap 'kill "$DEV_PID" 2>/dev/null || true' EXIT
  wait_for_url "Multica API" "http://localhost:${PORT}/health" "$DEV_PID" \
    || fail "Multica API did not answer. Read the make dev output above."
  wait_for_url "Multica web" "http://localhost:${FRONTEND_PORT}/" "$DEV_PID" \
    || fail "Multica web did not answer. Read the make dev output above."
  print_summary
  # make dev runs until it is stopped; keep this script attached to it.
  wait "$DEV_PID"
}

main() {
  check_prerequisites
  resolve_env_file
  fill_knowledge_secrets
  check_required_knowledge
  resolve_knowledge_urls
  load_env
  start_knowledge
  start_multica
}

main "$@"
