#!/usr/bin/env bash
# Install + register the GitHub Actions self-hosted runner that powers
# Multica's auto-deploy CD on the Mac mini production host.
#
# Why this lives here, not in the GH UI:
#   GitHub's "new runner" page hands you a 7-step copy-paste sequence
#   that's easy to skim-typo. This script wraps it as one command,
#   pins the version, sets the right labels, installs as a launchd
#   service, and verifies the daemon is online. Re-running it is
#   idempotent (it skips registration if the runner already exists).
#
# Usage:
#   1. On the Mac mini, open https://github.com/prellr/multica/settings/actions/runners/new
#   2. Copy the registration TOKEN (the value after `--token`).
#   3. Run: ./scripts/install-mac-mini-runner.sh
#   4. Paste the token when prompted.
#
# Re-running:
#   The runner caches under ~/actions-runner. Re-running with a fresh
#   token re-registers. To fully uninstall, see TEARDOWN at the bottom.
set -euo pipefail

# Pin a version so a future runner update doesn't silently change
# behavior under our feet. Update this string + the matching sha256
# when you intentionally bump.
RUNNER_VERSION="2.319.1"
RUNNER_DIR="${HOME}/actions-runner"
REPO_URL="https://github.com/prellr/multica"
RUNNER_NAME="$(hostname -s)-prod"
LABELS="self-hosted,macOS,mac-mini-prod"

arch="$(uname -m)"
case "${arch}" in
  arm64)  RUNNER_FILE="actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz" ;;
  x86_64) RUNNER_FILE="actions-runner-osx-x64-${RUNNER_VERSION}.tar.gz" ;;
  *) echo "Unsupported arch: ${arch}"; exit 1 ;;
esac
RUNNER_URL="https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${RUNNER_FILE}"

echo "=== Multica Mac-mini runner installer ==="
echo "Version: ${RUNNER_VERSION} (${arch})"
echo "Target:  ${RUNNER_DIR}"
echo "Repo:    ${REPO_URL}"
echo "Name:    ${RUNNER_NAME}"
echo "Labels:  ${LABELS}"
echo

# Step 1 — download + extract. Skip if the binary already exists at
# this exact version (idempotent re-runs).
if [[ -x "${RUNNER_DIR}/run.sh" ]] && [[ "$("${RUNNER_DIR}/config.sh" --version 2>/dev/null || true)" == *"${RUNNER_VERSION}"* ]]; then
  echo "[1/4] Runner v${RUNNER_VERSION} already present at ${RUNNER_DIR}; skipping download"
else
  echo "[1/4] Downloading runner v${RUNNER_VERSION}..."
  mkdir -p "${RUNNER_DIR}"
  cd "${RUNNER_DIR}"
  curl -fsSL -o "${RUNNER_FILE}" "${RUNNER_URL}"
  tar xzf "${RUNNER_FILE}"
  rm -f "${RUNNER_FILE}"
  echo "    Extracted to ${RUNNER_DIR}"
fi

cd "${RUNNER_DIR}"

# Step 2 — register. If already configured, ask before re-registering
# (which would unregister the old name first).
if [[ -f ".runner" ]]; then
  echo "[2/4] Runner already configured (.runner present)."
  echo "      Existing name: $(jq -r .agentName .runner 2>/dev/null || echo '?')"
  echo "      To re-register with a fresh token, first run:"
  echo "        cd ${RUNNER_DIR} && ./config.sh remove --token <REMOVAL_TOKEN>"
  echo "      Skipping config step."
else
  echo "[2/4] Need a runner registration token."
  echo "      Open: ${REPO_URL}/settings/actions/runners/new"
  echo "      Copy the value after \`--token\` (NOT a personal access token)."
  read -r -p "      Paste token: " TOKEN
  if [[ -z "${TOKEN}" ]]; then
    echo "No token provided; aborting."
    exit 1
  fi
  ./config.sh \
    --url "${REPO_URL}" \
    --token "${TOKEN}" \
    --name "${RUNNER_NAME}" \
    --labels "${LABELS}" \
    --work "_work" \
    --unattended \
    --replace
  echo "    Registered as: ${RUNNER_NAME}"
fi

# Step 3 — install + start as a launchd service so it survives reboots
# and the user logging out. The runner's own `svc.sh` wraps the
# /Library/LaunchDaemons plist generation; we just trust it.
echo "[3/4] Installing launchd service..."
if launchctl list | grep -q "actions.runner"; then
  echo "    Service already present; restarting to pick up any changes"
  ./svc.sh stop || true
  ./svc.sh start
else
  ./svc.sh install
  ./svc.sh start
fi

# Step 4 — verify. Poll the runner status for up to 30s.
echo "[4/4] Verifying runner is online..."
for i in $(seq 1 15); do
  status="$(./svc.sh status 2>&1 || true)"
  if echo "${status}" | grep -q "started"; then
    echo "    Runner reports: started"
    echo
    echo "=== Done. ==="
    echo "Check the runner appears as 'Idle' at:"
    echo "  ${REPO_URL}/settings/actions/runners"
    echo
    echo "Then dispatch a test deploy:"
    echo "  gh workflow run deploy-production.yml --repo prellr/multica"
    exit 0
  fi
  sleep 2
done

echo "Runner didn't report 'started' within 30s. Last status:"
echo "${status}"
exit 1

# ---------------------------------------------------------------------
# TEARDOWN (kept here as documentation, not run by the script):
#
#   cd ~/actions-runner
#   ./svc.sh stop
#   ./svc.sh uninstall
#   ./config.sh remove --token <REMOVAL_TOKEN_FROM_GH>
#   cd ~ && rm -rf actions-runner
#
# Removal token is at the same URL as the registration token, just
# generated separately when an active runner exists.
# ---------------------------------------------------------------------
