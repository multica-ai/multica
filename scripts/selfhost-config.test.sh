#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# docker-compose.selfhost.yml requires JWT_SECRET (${JWT_SECRET:?...}) and
# .env.example ships an empty value. Direct `docker compose config` calls in
# this script let the calling environment outrank the env file, so one
# throwaway export covers them. The make-driven recipes below additionally
# include .env and bare-`export` it to the recipe environment, clobbering
# this value, so run_recipe seeds it into the recipe .env as well.
export JWT_SECRET=test-secret-for-config-test

require_config() {
  local config=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$config"; then
    echo "Missing expected docker compose config value:"
    echo "  $expected"
    exit 1
  fi
}

require_env() {
  local output=$1
  local expected=$2

  if ! grep -Fxq "$expected" <<<"$output"; then
    echo "Missing expected derived env value:"
    echo "  $expected"
    echo "Observed:"
    echo "$output"
    exit 1
  fi
}

tmp_env="$(mktemp)"
tmp_dir="$(mktemp -d)"
trap 'rm -f "$tmp_env"; rm -rf "$tmp_dir"' EXIT
sed 's/^FRONTEND_PORT=.*/FRONTEND_PORT=3100/' .env.example >"$tmp_env"
printf '\nBACKEND_PORT=9100\nSMTP_FROM_EMAIL=multica@example.com\n' >>"$tmp_env"
printf 'MULTICA_LLM_API_KEY=llm-key-from-env\nMULTICA_LLM_BASE_URL=http://gateway.example/v1\nMULTICA_LLM_DEFAULT_MODEL=model-from-env\nMULTICA_LLM_MAX_RETRIES=3\n' >>"$tmp_env"

config="$(
  docker compose \
    --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml \
    config
)"

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOGLE_REDIRECT_URI: http://localhost:3100/auth/callback'
require_config "$config" 'MULTICA_APP_URL: http://localhost:3100'
require_config "$config" 'SMTP_FROM_EMAIL: multica@example.com'

# The backend environment is an explicit allowlist, so a variable documented in
# .env.example but missing here silently never reaches the container: the
# operator configures it, the server never sees it, and nothing reports the gap.
# Assert the values actually land, then assert the allowlist has not drifted
# behind the documentation the next time an LLM knob is added.
require_config "$config" 'MULTICA_LLM_API_KEY: llm-key-from-env'
require_config "$config" 'MULTICA_LLM_BASE_URL: http://gateway.example/v1'
require_config "$config" 'MULTICA_LLM_DEFAULT_MODEL: model-from-env'
require_config "$config" 'MULTICA_LLM_MAX_RETRIES: "3"'

while IFS= read -r llm_var; do
  if ! grep -Eq "^[[:space:]]+${llm_var}: \\\$\{${llm_var}:-" docker-compose.selfhost.yml; then
    echo "$llm_var is documented in .env.example but not mapped into the backend"
    echo "service in docker-compose.selfhost.yml, so self-hosted deployments cannot set it."
    exit 1
  fi
done < <(grep -oE '^MULTICA_LLM_[A-Z_]+' .env.example)

for script in scripts/dev.sh scripts/check.sh; do
  if ! grep -Fq '. scripts/local-env.sh' "$script"; then
    echo "$script must source scripts/local-env.sh for shared local env derivation."
    exit 1
  fi
done

local_env="$(
  env -i PATH="$PATH" bash -c '
    set -euo pipefail
    env_file=$1
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
    # shellcheck disable=SC1091
    . scripts/local-env.sh
    printf "%s\n" \
      "PORT=${PORT}" \
      "FRONTEND_PORT=${FRONTEND_PORT}" \
      "FRONTEND_ORIGIN=${FRONTEND_ORIGIN}" \
      "MULTICA_APP_URL=${MULTICA_APP_URL}" \
      "GOOGLE_REDIRECT_URI=${GOOGLE_REDIRECT_URI}" \
      "MULTICA_SERVER_URL=${MULTICA_SERVER_URL}" \
      "LOCAL_UPLOAD_BASE_URL=${LOCAL_UPLOAD_BASE_URL}" \
      "PLAYWRIGHT_BASE_URL=${PLAYWRIGHT_BASE_URL}"
  ' _ "$tmp_env"
)"

require_env "$local_env" 'PORT=9100'
require_env "$local_env" 'FRONTEND_PORT=3100'
require_env "$local_env" 'FRONTEND_ORIGIN=http://localhost:3100'
require_env "$local_env" 'MULTICA_APP_URL=http://localhost:3100'
require_env "$local_env" 'GOOGLE_REDIRECT_URI=http://localhost:3100/auth/callback'
require_env "$local_env" 'MULTICA_SERVER_URL=ws://localhost:9100/ws'
require_env "$local_env" 'LOCAL_UPLOAD_BASE_URL=http://localhost:9100'
require_env "$local_env" 'PLAYWRIGHT_BASE_URL=http://localhost:3100'

worktree_env="$tmp_dir/.env.worktree"
WORKTREE_NAME=selfhost-config-test bash scripts/init-worktree-env.sh "$worktree_env" >/dev/null
worktree_backend_port="$(sed -n 's/^PORT=//p' "$worktree_env")"
require_env "$(cat "$worktree_env")" "MULTICA_PUBLIC_URL=http://localhost:${worktree_backend_port}"

resolve_local_public_url() {
  env -i PATH="$PATH" bash -c '
    set -euo pipefail
    env_file=$1
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
    # shellcheck disable=SC1091
    . scripts/local-env.sh
    printf "%s\n" "$MULTICA_PUBLIC_URL"
  ' _ "$1"
}

make_env_probe="$tmp_dir/print-public-url.mk"
printf '%s\n' \
  '.PHONY: print-public-url' \
  'print-public-url:' \
  '	@printf "%s\n" "$$MULTICA_PUBLIC_URL"' \
  >"$make_env_probe"

resolve_make_public_url() {
  make \
    --no-print-directory \
    -s \
    -f Makefile \
    -f "$make_env_probe" \
    ENV_FILE="$1" \
    print-public-url
}

old_worktree_env="$tmp_dir/.env.worktree.old"
grep -v '^MULTICA_PUBLIC_URL=' "$worktree_env" >"$old_worktree_env"
require_env \
  "$(resolve_local_public_url "$old_worktree_env")" \
  "http://localhost:${worktree_backend_port}"
require_env \
  "$(resolve_make_public_url "$old_worktree_env")" \
  "http://localhost:${worktree_backend_port}"

explicit_worktree_env="$tmp_dir/.env.worktree.explicit"
cp "$old_worktree_env" "$explicit_worktree_env"
printf '\nMULTICA_PUBLIC_URL=https://api.explicit.example\n' >>"$explicit_worktree_env"
require_env \
  "$(resolve_local_public_url "$explicit_worktree_env")" \
  "https://api.explicit.example"
require_env \
  "$(resolve_make_public_url "$explicit_worktree_env")" \
  "https://api.explicit.example"

# ---------------------------------------------------------------------------
# Host port consistency (regression for #6145)
#
# The health check and the success message must use the port Compose actually
# published. The recipes used to probe ${PORT:-8080} while Compose published
# ${BACKEND_PORT:-8080} — two sources of truth, so any config where they
# disagreed hammered the wrong port for 60s and then reported "Services are
# still starting" on a healthy stack.
#
# These cases drive the real `make selfhost` recipe. The docker stub does NOT
# get told what to publish: on `up` it asks the real `docker compose config` to
# interpolate the compose file with the environment the recipe actually handed
# it, records that, and answers `port` from the recording. So the assertion
# crosses environment -> make -> compose -> report for real, and a stub that
# agreed with a broken recipe could not make it pass.
# ---------------------------------------------------------------------------

stub_dir="$tmp_dir/bin"
mkdir -p "$stub_dir"

cat >"$stub_dir/docker" <<'STUB'
#!/usr/bin/env bash
# `docker compose` stub. Derives the published host ports from the real compose
# interpolation of the environment this invocation received, so the recorded
# answer is whatever the recipe truly asked Compose for.
set -uo pipefail
args=("$@")
sub=""
subidx=0
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[i]}" in
  up | pull | port | version | logs | down | build)
    sub=${args[i]}
    subidx=$i
    break
    ;;
  esac
done
files=()
for ((i = 0; i < ${#args[@]}; i++)); do
  if [ "${args[i]}" = "-f" ]; then files+=(-f "${args[i + 1]}"); fi
done
case "$sub" in
version) echo "2.30.0" ;;
up)
  "$REAL_DOCKER" compose "${files[@]}" config --format json 2>/dev/null | node -e '
let raw = "";
process.stdin.on("data", (chunk) => (raw += chunk));
process.stdin.on("end", () => {
  const config = JSON.parse(raw);
  for (const service of ["backend", "frontend"]) {
    console.log(service + "=" + config.services[service].ports[0].published);
  }
});
' >>"$STUB_PUBLISHED_RECORD"
  ;;
port)
  service=${args[subidx + 1]}
  published=$(grep "^${service}=" "$STUB_PUBLISHED_RECORD" 2>/dev/null | tail -n 1 | cut -d= -f2)
  if [ -z "$published" ]; then exit 1; fi
  printf '127.0.0.1:%s\n' "$published"
  ;;
*) : ;;
esac
exit 0
STUB

cat >"$stub_dir/curl" <<'STUB'
#!/usr/bin/env bash
# Records probed URLs and always reports a healthy backend.
set -uo pipefail
for arg in "$@"; do
  case "$arg" in
  http*) printf '%s\n' "$arg" >>"$STUB_CURL_LOG" ;;
  esac
done
exit 0
STUB

chmod +x "$stub_dir/docker" "$stub_dir/curl"

# Throwaway checkout so the recipe never touches this repo's own .env.
recipe_dir="$tmp_dir/recipe"
mkdir -p "$recipe_dir/scripts"
cp Makefile .env.example docker-compose.selfhost.yml docker-compose.selfhost.build.yml "$recipe_dir/"
cp scripts/selfhost-wait.sh "$recipe_dir/scripts/"

record="$tmp_dir/published"
curl_log="$tmp_dir/probed"
real_docker="$(command -v docker)"

# Runs `make <target>` against the stubs after applying a sed script to .env.
# Remaining args are passed through to make (environment assignments must be
# given as VAR=value before the target via `env`, make variables after it).
run_recipe() {
  local target=$1 env_mutation=$2 shell_env=$3 make_args=$4

  cp "$recipe_dir/.env.example" "$recipe_dir/.env"
  # The Makefile includes .env and bare-`export`s every variable to the
  # recipe environment, which clobbers this script's JWT_SECRET with the
  # empty value .env.example ships; docker-compose.selfhost.yml now refuses
  # to interpolate an empty JWT_SECRET. Seed the throwaway value into the
  # recipe .env so make, the stub, and Compose all see a usable secret.
  sed "s/^JWT_SECRET=.*/JWT_SECRET=$JWT_SECRET/" "$recipe_dir/.env" >"$recipe_dir/.env.tmp"
  mv "$recipe_dir/.env.tmp" "$recipe_dir/.env"
  if [ -n "$env_mutation" ]; then
    sed "$env_mutation" "$recipe_dir/.env" >"$recipe_dir/.env.tmp"
    mv "$recipe_dir/.env.tmp" "$recipe_dir/.env"
  fi
  : >"$record"
  : >"$curl_log"

  (
    cd "$recipe_dir" || exit 1
    eval "env PATH=\"$stub_dir:\$PATH\" \
      REAL_DOCKER=\"$real_docker\" \
      STUB_PUBLISHED_RECORD=\"$record\" \
      STUB_CURL_LOG=\"$curl_log\" \
      $shell_env make $target $make_args"
  )
}

published_port() {
  grep "^$1=" "$record" | tail -n 1 | cut -d= -f2
}

probed_port() {
  sed -n '1s#http://localhost:\([0-9]*\)/health#\1#p' "$curl_log"
}

require_consistent() {
  local label=$1 expected=$2
  local published probed
  published=$(published_port backend)
  probed=$(probed_port)

  if [ "$published" != "$expected" ] || [ "$probed" != "$expected" ]; then
    echo "[$label] host port disagreement"
    echo "  expected published and probed port: $expected"
    echo "  compose published: ${published:-<none>}"
    echo "  health check probed: ${probed:-<none>}"
    exit 1
  fi
}

# PORT is the value to edit, so editing it must move the published port and the
# probe together. Fails on the old recipe, which probed 9100 while Compose
# published 8080.
run_recipe selfhost 's/^PORT=8080/PORT=9100/' '' '' >/dev/null
require_consistent 'PORT edited in .env' 9100

# BACKEND_PORT remains an alias that overrides PORT.
run_recipe selfhost 's/^# BACKEND_PORT=8080/BACKEND_PORT=9200/' '' '' >/dev/null
require_consistent 'BACKEND_PORT alias in .env' 9200

# A make command-line override must not desync the probe from Compose. Fails on
# the old recipe, which probed 8080 while Compose published 9100.
run_recipe selfhost 's/^# BACKEND_PORT=8080/BACKEND_PORT=9100/' '' 'PORT=8080' >/dev/null
require_consistent 'make PORT=8080 over BACKEND_PORT=9100' 9100

# With no alias pinned in .env, an alias from the environment survives make's
# include and takes effect end to end.
run_recipe selfhost '' 'BACKEND_PORT=9300' '' >/dev/null
require_consistent 'BACKEND_PORT from the environment' 9300

# The env file stays authoritative for values it does set, so an environment
# PORT loses to it — and the probe must follow whatever Compose then published.
run_recipe selfhost '' 'PORT=9500' '' >/dev/null
require_consistent 'PORT from the environment is overridden by .env' 8080

# Defaults stay 8080/3000.
run_recipe selfhost '' '' '' >/dev/null
require_consistent 'defaults' 8080
if [ "$(published_port frontend)" != "3000" ]; then
  echo "default frontend host port should be 3000, got $(published_port frontend)"
  exit 1
fi

# selfhost-build resolves the port the same way.
run_recipe selfhost-build 's/^PORT=8080/PORT=9400/' '' '' >/dev/null
require_consistent 'selfhost-build with PORT edited' 9400

# Every alias at once: BACKEND_PORT wins, and the probe follows it.
run_recipe selfhost \
  's/^PORT=8080/PORT=9000/;s/^# BACKEND_PORT=8080/BACKEND_PORT=8000/;s/^# API_PORT=8080/API_PORT=7000/;s/^# SERVER_PORT=8080/SERVER_PORT=6000/' \
  '' '' >/dev/null
require_consistent 'every alias set at once' 8000

# A higher-priority alias in the env file beats a lower-priority one from the
# shell.
run_recipe selfhost \
  's/^PORT=8080/PORT=8000/;s/^# BACKEND_PORT=8080/BACKEND_PORT=8000/' \
  'API_PORT=7000' '' >/dev/null
require_consistent 'env-file BACKEND_PORT over shell API_PORT' 8000

# ---------------------------------------------------------------------------
# Make command-line variables
#
# A command-line assignment outranks both the env file and the environment, and
# it is a third origin that no re-derivation of the port got right. Since the
# recipe now asks Compose, the probe follows whatever actually got published.
# ---------------------------------------------------------------------------

# Command-line high-priority alias over a low-priority alias in the env file.
run_recipe selfhost 's/^# API_PORT=8080/API_PORT=7000/' '' 'BACKEND_PORT=9000' >/dev/null
require_consistent 'command-line BACKEND_PORT over env-file API_PORT' 9000

# Env-file high-priority alias over a low-priority alias on the command line.
run_recipe selfhost 's/^# BACKEND_PORT=8080/BACKEND_PORT=9100/' '' 'SERVER_PORT=6000' >/dev/null
require_consistent 'env-file BACKEND_PORT over command-line SERVER_PORT' 9100

# An explicit empty value on the command line drops out of the chain.
run_recipe selfhost '' '' 'BACKEND_PORT=' >/dev/null
require_consistent 'command-line BACKEND_PORT= falls through to PORT' 8080

# ---------------------------------------------------------------------------
# Explicit empty assignments in the env file
#
# `BACKEND_PORT=` is a distinct input from an absent one: make lets it override
# the same variable from the environment, and it drops out of the alias chain
# instead of setting a port. Compose treats the exported empty value the same
# way, so the two agree.
# ---------------------------------------------------------------------------

run_recipe selfhost 's/^# BACKEND_PORT=8080/BACKEND_PORT=/' 'BACKEND_PORT=9000' '' >/dev/null
require_consistent 'empty BACKEND_PORT in .env over shell BACKEND_PORT' 8080

run_recipe selfhost 's/^PORT=8080/PORT=/;s/^# BACKEND_PORT=8080/BACKEND_PORT=/' 'PORT=9000' '' >/dev/null
require_consistent 'every chain variable emptied in .env' 8080

run_recipe selfhost 's/^FRONTEND_PORT=3000/FRONTEND_PORT=/' 'FRONTEND_PORT=3100' '' >/dev/null
if [ "$(published_port frontend)" != "3000" ]; then
  echo "an empty FRONTEND_PORT in .env must fall back to 3000, got $(published_port frontend)"
  exit 1
fi

# The recipes must delegate instead of re-deriving the port.
for expected_call in 'bash scripts/selfhost-wait.sh official' 'bash scripts/selfhost-wait.sh build'; do
  if ! grep -Fq "$expected_call" Makefile; then
    echo "Makefile must call the shared wait script: $expected_call"
    exit 1
  fi
done
if grep -n 'localhost:$${PORT' Makefile; then
  echo "The self-host recipes must not re-derive the backend host port from \$PORT."
  echo "Use scripts/selfhost-wait.sh, which reads the published port from Compose."
  exit 1
fi

# ---------------------------------------------------------------------------
# The backend port alias chain, and Compose's own source precedence
#
#   BACKEND_PORT -> API_PORT -> SERVER_PORT -> PORT -> 8080
#
# Exercised through the direct Compose path, because the Makefile normalises all
# four into PORT before a recipe ever runs — so a recipe-only test cannot see a
# gap here.
#
# This is also where Compose's precedence is pinned against the real binary: the
# calling environment outranks the env file. Both installers relied on their own
# .env-only derivation and so probed a port Compose never published (#6145);
# they now ask `docker compose port` instead, which is asserted end to end in
# scripts/install.test.sh and scripts/install.ps1.test.ps1. Those suites run on
# agents without a Docker CLI, so the ground truth for precedence lives here.
# ---------------------------------------------------------------------------

# Neither installer may reconstruct the port from the env file again.
for installer in scripts/install.sh scripts/install.ps1; do
  if grep -nE '(selfhost_backend_port|selfhost_frontend_port|Get-SelfHostBackendPort|Get-SelfHostFrontendPort)' "$installer"; then
    echo "$installer must not re-derive host ports from .env."
    echo "Read the published port from Compose, as scripts/selfhost-wait.sh does."
    exit 1
  fi
done
for installer_call in \
  'compose_published_port backend 8080' \
  'compose_published_port frontend 3000'; do
  if ! grep -Fq "$installer_call" scripts/install.sh; then
    echo "scripts/install.sh must read the published port from Compose: $installer_call"
    exit 1
  fi
done
for installer_call in \
  'Get-ComposePublishedPort -Service "backend" -ContainerPort 8080' \
  'Get-ComposePublishedPort -Service "frontend" -ContainerPort 3000'; do
  if ! grep -Fq "$installer_call" scripts/install.ps1; then
    echo "scripts/install.ps1 must read the published port from Compose: $installer_call"
    exit 1
  fi
done

compose_published_ports() {
  local env_file=$1
  shift
  env "$@" docker compose --env-file "$env_file" -f docker-compose.selfhost.yml config --format json |
    node -e '
let raw = "";
process.stdin.on("data", (chunk) => (raw += chunk));
process.stdin.on("end", () => {
  const config = JSON.parse(raw);
  console.log(config.services.backend.ports[0].published + " " + config.services.frontend.ports[0].published);
});
'
}

# Each case: label, sed applied to .env.example, ambient env, expected backend,
# expected frontend. .env.example ships PORT=8080 with every alias commented out.
while IFS='|' read -r case_label case_mutation case_ambient case_backend case_frontend; do
  [ -n "$case_label" ] || continue

  case_env="$tmp_dir/.env.alias"
  if [ -n "$case_mutation" ]; then
    sed "$case_mutation" .env.example >"$case_env"
  else
    cp .env.example "$case_env"
  fi

  # Unset every port variable first so the agent's own environment cannot leak.
  read -r observed_backend observed_frontend < <(
    compose_published_ports "$case_env" \
      -u PORT -u BACKEND_PORT -u API_PORT -u SERVER_PORT -u FRONTEND_PORT \
      ${case_ambient:+"$case_ambient"}
  )

  if [ "$observed_backend" != "$case_backend" ] || [ "$observed_frontend" != "$case_frontend" ]; then
    echo "[$case_label] Compose published an unexpected host port"
    echo "  expected: backend=$case_backend frontend=$case_frontend"
    echo "  observed: backend=$observed_backend frontend=$observed_frontend"
    exit 1
  fi
done <<'CASES'
defaults|||8080|3000
PORT only|s/^PORT=8080/PORT=9100/||9100|3000
SERVER_PORT overrides PORT|s/^# SERVER_PORT=8080/SERVER_PORT=9200/||9200|3000
API_PORT overrides SERVER_PORT|s/^# API_PORT=8080/API_PORT=9300/;s/^# SERVER_PORT=8080/SERVER_PORT=9200/||9300|3000
BACKEND_PORT overrides all|s/^# BACKEND_PORT=8080/BACKEND_PORT=9400/;s/^# API_PORT=8080/API_PORT=9300/;s/^# SERVER_PORT=8080/SERVER_PORT=9200/||9400|3000
ambient PORT beats the env file|s/^PORT=8080/PORT=9100/|PORT=9500|9500|3000
ambient BACKEND_PORT beats the env file|s/^PORT=8080/PORT=9100/|BACKEND_PORT=9600|9600|3000
ambient API_PORT beats the env file|s/^PORT=8080/PORT=9100/|API_PORT=9700|9700|3000
ambient SERVER_PORT beats the env file|s/^PORT=8080/PORT=9100/|SERVER_PORT=9800|9800|3000
ambient FRONTEND_PORT beats the env file|s/^FRONTEND_PORT=3000/FRONTEND_PORT=3100/|FRONTEND_PORT=3200|8080|3200
CASES

# An env-file alias beats the same alias from the environment, and the probe
# follows whatever Compose published either way.
for shadowed_alias in BACKEND_PORT API_PORT SERVER_PORT; do
  run_recipe selfhost "s/^# ${shadowed_alias}=8080/${shadowed_alias}=9700/" \
    "${shadowed_alias}=9600" '' >/dev/null
  require_consistent "env-file ${shadowed_alias} over the same shell variable" 9700
done

echo "self-host env derivation ok"
