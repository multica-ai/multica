"""Modal Labs deploy wrapper for the cerebro-inference FastAPI app, hviske-only.

This is the tale→tekst (speech-to-text) deploy: it runs hviske-v3-conversation
alone and skips plapre entirely (`INFERENCE_ENABLE_PLAPRE=false`), so the image
needs neither the plapre git package nor vLLM. The combined TTS+STT deploy lives
in `modal_app.py` (JEH-740).

Deploy (requires the `modal` CLI authenticated against the Multica org):

    cd cerebro-inference/
    modal secret create cerebro-hviske-secrets \\
        INFERENCE_API_KEY=<random> HF_TOKEN=<hf token>
    modal deploy modal_hviske.py

After deploy, Modal prints the public ASGI URL. The hviske endpoint is then
`POST <url>/hviske/transcribe` (multipart `file=@clip`, bearer = INFERENCE_API_KEY).
Set the URL on the multica backend for the dictation proxy once the chat is wired.

Frankfurt (EU) region keeps the audio path GDPR-friendly. L4 GPU runs large-v3
comfortably; scale-to-zero means we only pay while a clip is being transcribed.
"""

from __future__ import annotations

import modal

image = (
    modal.Image.from_registry(
        "nvidia/cuda:12.1.1-cudnn8-runtime-ubuntu22.04", add_python="3.12"
    )
    .apt_install("git", "libsndfile1", "ffmpeg", "build-essential")
    .pip_install(
        "fastapi>=0.115,<1.0",
        "uvicorn[standard]>=0.32,<1.0",
        "pydantic>=2.9,<3.0",
        "pydantic-settings>=2.6,<3.0",
        "huggingface_hub>=0.26,<1.0",
        "numpy>=2.0,<3.0",
        "python-multipart>=0.0.12,<1.0",
        "soundfile>=0.12,<1.0",
    )
    .pip_install(
        "torch>=2.5,<3.0",
        "transformers>=4.46,<5.0",
        "safetensors>=0.4,<1.0",
        extra_index_url="https://download.pytorch.org/whl/cu121",
    )
    .env(
        {
            "HF_HOME": "/cache/hf",
            "HUGGINGFACE_HUB_CACHE": "/cache/hf",
            # hviske-only: skip plapre so neither vLLM nor the plapre git package
            # is needed in this image.
            "INFERENCE_ENABLE_PLAPRE": "false",
            "INFERENCE_ENABLE_HVISKE": "true",
        }
    )
    # add_local_* must be the LAST image step (Modal adds these on container
    # start, not as a cached build layer).
    .add_local_python_source("app")
)

# Persistent HF cache so hviske-v3 (~3 GB) doesn't re-download on every cold boot.
model_cache = modal.Volume.from_name("cerebro-hviske-cache", create_if_missing=True)

# Secrets owned by the Multica Modal workspace (INFERENCE_API_KEY + HF_TOKEN).
secrets = modal.Secret.from_name("cerebro-hviske-secrets")
# FIR-1797 transcript cleanup: FIRTAL_AI_GATEWAY_URL + FIRTAL_AI_GATEWAY_KEY for
# the OpenAI-compatible proxy. Kept as a separate secret so the inference key
# secret above stays untouched. Cleanup is best-effort: if this is absent the
# raw transcript is returned unchanged.
gateway_secret = modal.Secret.from_name("cerebro-hviske-gateway")

app = modal.App("cerebro-hviske")


@app.function(
    image=image,
    gpu="L4",
    region="eu",  # Frankfurt. GDPR-safe audio path.
    volumes={"/cache/hf": model_cache},
    secrets=[secrets, gateway_secret],
    scaledown_window=300,
    max_containers=4,
)
@modal.asgi_app()
def fastapi_app():
    """Serve the existing FastAPI app under Modal's ASGI runner.

    Imported inside the function so Modal's image builder doesn't need torch on
    the deploy box — only the GPU container loads it. The import re-runs on cold
    boot with the secret-injected HF_TOKEN already in env, so hviske weights pull
    with auth on first request."""
    from app.main import app as fastapi_instance

    return fastapi_instance
