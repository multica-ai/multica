"""Pre-download models so the first /synthesize doesn't pay the GB-scale pull.

Runs at container start (entrypoint.sh) when INFERENCE_MOCK_MODE=0 and
INFERENCE_SKIP_PREFETCH=0. Also doubles as a CLI for warming a volume mount
locally — see cerebro-inference/README.md.

Plapre-nano is gated on Hugging Face (CC-BY-4.0). HF_TOKEN must belong to an
account that has accepted the license. The hviske pull is a no-op for now —
JEH-729 slice 4 swaps the placeholder for the real ID + adds the snapshot pull.
"""

from __future__ import annotations

import logging
import os
import sys

log = logging.getLogger("multica.inference.prefetch")
logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())


def _snapshot(model_id: str, token: str | None) -> None:
    from huggingface_hub import snapshot_download

    log.info("prefetch %s", model_id)
    snapshot_download(repo_id=model_id, token=token, local_files_only=False)


def main() -> int:
    token = os.environ.get("HF_TOKEN") or None
    plapre_id = os.environ.get("PLAPRE_MODEL_ID", "syvai/plapre-nano")

    if not token:
        log.error("HF_TOKEN is required to pull %s (gated)", plapre_id)
        return 1

    try:
        _snapshot(plapre_id, token)
    except Exception:
        log.exception("plapre prefetch failed")
        return 2

    # Hviske prefetch is intentionally absent; JEH-729 slice 4 adds it.
    return 0


if __name__ == "__main__":
    sys.exit(main())
