#!/usr/bin/env bash
# Wrapper: install company-harness from multica repo root.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
exec bash "$ROOT/.ai-company/harness/install.sh" "$@"
