#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dockerfile="$repo_root/Dockerfile.runtime"
entrypoint="$repo_root/docker/runtime-entrypoint.sh"
runbook="$repo_root/CLOUD_RUNTIME.md"
pi_gateway="$repo_root/docker/pi-firtal-gateway.ts"
canary="$repo_root/scripts/cerebro/runtime-agent-canary.sh"

assert_contains() {
  local file="$1" pattern="$2" message="$3"
  if ! grep -Eq "$pattern" "$file"; then
    printf 'FAIL: %s\n' "$message" >&2
    exit 1
  fi
}

assert_contains "$dockerfile" '@earendil-works/pi-coding-agent@0\.80\.7' \
  'Pi must remain pinned at 0.80.7'
assert_contains "$dockerfile" 'hermes-agent\[acp\]==0\.18\.2' \
  'Hermes Agent must be installed at pinned release 0.18.2 with the acp extra (hermes acp exits 1 without it)'
assert_contains "$dockerfile" 'ARG CURSOR_AGENT_VERSION=2026\.07\.20-8cc9c0b' \
  'Cursor Agent must remain pinned at the reviewed 2026.07.20 release'
assert_contains "$dockerfile" 'downloads\.cursor\.com/lab/\$\{CURSOR_AGENT_VERSION\}/linux/\$\{cursor_arch\}/agent-cli-package\.tar\.gz' \
  'Cursor Agent must be installed from the pinned official package'
assert_contains "$dockerfile" 'x86_64\|amd64.*cursor_arch=x64' \
  'Cursor Agent install must support x64 runtime images'
assert_contains "$dockerfile" 'aarch64\|arm64.*cursor_arch=arm64' \
  'Cursor Agent install must support arm64 runtime images'
assert_contains "$dockerfile" 'cursor-agent --version' \
  'Cursor Agent must be executable during the image build'
assert_contains "$dockerfile" 'MULTICA_PI_PATH=/usr/local/bin/pi' \
  'MULTICA_PI_PATH must point at the image Pi binary'
assert_contains "$dockerfile" 'MULTICA_HERMES_PATH=/usr/local/bin/hermes' \
  'MULTICA_HERMES_PATH must point at the image Hermes binary'
assert_contains "$dockerfile" 'MULTICA_CURSOR_PATH=/usr/local/bin/cursor-agent' \
  'MULTICA_CURSOR_PATH must point at the image Cursor Agent binary'
assert_contains "$dockerfile" 'PI_CODING_AGENT_DIR=/home/multica/\.multica/pi' \
  'Pi state must live on the persistent Multica volume'
assert_contains "$dockerfile" 'HERMES_HOME=/home/multica/\.multica/hermes' \
  'Hermes state must live on the persistent Multica volume'
assert_contains "$entrypoint" 'MULTICA_PI_PATH.*MULTICA_HERMES_PATH.*MULTICA_CURSOR_PATH' \
  'runtime entrypoint must preflight Pi, Hermes, and Cursor Agent binaries'
assert_contains "$dockerfile" 'configure-hermes-fallback\.py' \
  'runtime image must include the Hermes gateway fallback configurator'
assert_contains "$entrypoint" 'configure-hermes-fallback\.py' \
  'runtime entrypoint must configure the Hermes gateway fallback'
assert_contains "$dockerfile" 'pi-firtal-gateway\.ts' \
  'runtime image must include the Pi gateway provider extension'
assert_contains "$entrypoint" 'pi-firtal-gateway\.ts' \
  'runtime entrypoint must install the Pi gateway provider extension'
assert_contains "$dockerfile" 'runtime-agent-canary\.sh /usr/local/bin/runtime-agent-canary' \
  'runtime image must expose the PI and Hermes canary command'
assert_contains "$runbook" 'Both Pi and Hermes support the `openai-codex` provider' \
  'runbook must make ChatGPT Pro the documented primary for both agents'
assert_contains "$runbook" 'runtime-agent-canary all' \
  'runbook must require the combined post-deploy and post-restart canary'
assert_contains "$runbook" 'CURSOR_API_KEY' \
  'runbook must document Cursor Agent authentication'
assert_contains "$pi_gateway" 'claude-sonnet-5' \
  'Pi gateway provider must default to a model exposed by Firtal AI Gateway'
assert_contains "$canary" 'claude-sonnet-5' \
  'runtime canary must test the managed gateway model'
assert_contains "$runbook" 'Log out all.*does not manage' \
  'runbook must not claim that general ChatGPT logout revokes Codex OAuth'
assert_contains "$dockerfile" '^ +git ripgrep .*chromium' \
  'runtime image must install the distro chromium — a non-root agent cannot apt-get Chrome deps at runtime'
assert_contains "$dockerfile" 'agent-browser@0\.26\.0' \
  'agent-browser must stay pinned at 0.26.0, the same pin as Dockerfile and Dockerfile.browser-verifier'
assert_contains "$dockerfile" 'AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium' \
  'AGENT_BROWSER_EXECUTABLE_PATH must point at the image chromium binary'
assert_contains "$dockerfile" 'AGENT_BROWSER_ARGS=--no-sandbox,--disable-dev-shm-usage' \
  'Chrome must run without the sandbox — the container has no user namespaces'

printf 'Docker runtime pins Cursor Agent, Pi, and Hermes with persistent state\n'
