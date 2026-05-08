#!/bin/bash
set -euo pipefail

if [[ "${INFERENCE_MOCK_MODE:-0}" == "1" ]]; then
  echo "[entrypoint] INFERENCE_MOCK_MODE=1, skipping model prefetch" >&2
elif [[ "${INFERENCE_SKIP_PREFETCH:-0}" == "1" ]]; then
  echo "[entrypoint] INFERENCE_SKIP_PREFETCH=1, skipping prefetch" >&2
else
  echo "[entrypoint] prefetching models (HF_HOME=${HF_HOME:-/models})" >&2
  python /app/scripts/prefetch.py
fi

exec "$@"
