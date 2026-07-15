#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dockerfile="$repo_root/Dockerfile.runtime"

require_line() {
  local pattern="$1"
  local message="$2"
  if ! grep -Fq -- "$pattern" "$dockerfile"; then
    echo "$message" >&2
    exit 1
  fi
}

require_line 'npm install -g --ignore-scripts @earendil-works/pi-coding-agent@' \
  'Dockerfile.runtime must install the official Pi Coding Agent package at a pinned version'
require_line 'pi --version' \
  'Dockerfile.runtime must verify the Pi CLI during the image build'
require_line 'PI_CODING_AGENT_DIR=/home/multica/.multica/pi' \
  'Pi config must live on the cloud runtime persistent config volume'
require_line 'MULTICA_PI_PATH=/usr/local/bin/pi' \
  'The Multica daemon must use the Pi binary installed in the runtime image'

echo 'Dockerfile.runtime installs Pi with persistent authentication storage'
