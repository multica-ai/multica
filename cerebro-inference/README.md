# Multica inference container

Self-hosted TTS + STT for the cerebro voice extension. Single FastAPI process,
two model surfaces sharing one GPU:

| Route | Backed by | Status |
|-------|-----------|--------|
| `POST /plapre/synthesize` | `syvai/plapre-nano` (Danish TTS, CC-BY-4.0) | implemented (slice A — JEH-740) |
| `GET  /plapre/voices`     | static metadata | implemented |
| `POST /hviske/transcribe` | `syvai/hviske-v3-conversation` (Whisper fork) | stubbed — JEH-729 slice 4 fills in |
| `GET  /healthz`           | always 200 | implemented |
| `GET  /readyz`            | reports per-model load state | implemented |

The split image with both services lives here so plapre and hviske share GPU
memory, the prefetch step, and the auth/health surface — instead of running two
separate containers.

## Local dev (no GPU)

```bash
cd cerebro-inference
python3.12 -m venv .venv
source .venv/bin/activate
pip install -e '.[dev]'

INFERENCE_MOCK_MODE=1 \
INFERENCE_API_KEY=dev-secret \
uvicorn app.main:app --reload
```

Mock mode replaces both models with deterministic stand-ins (sine-wave PCM for
`/synthesize`, 503 for `/transcribe`). It exists so the frontend (slice C) and
the backend proxy (slice B) can be wired up end-to-end before the real models
are deployed, and so CI doesn't need a GPU.

```bash
curl -N -H "Authorization: Bearer dev-secret" \
     -H "Content-Type: application/json" \
     -d '{"text":"Hej fra Multica."}' \
     http://localhost:8000/plapre/synthesize \
  | ffplay -f s16le -ar 24000 -i -
```

Tests:

```bash
pytest
```

## Build the image

```bash
docker build -t ghcr.io/firtal-group/multica-inference:dev cerebro-inference/
```

The image bases on `nvidia/cuda:12.1.1-cudnn8-runtime-ubuntu22.04` with Python
3.12 from deadsnakes (plapre requires 3.12+). Pinned `torch+cu121` wheels —
host needs CUDA 12.1+ drivers.

## Run the image

```bash
docker run --rm --gpus all -p 8000:8000 \
  -e INFERENCE_API_KEY="$(openssl rand -hex 32)" \
  -e HF_TOKEN="$HF_TOKEN" \
  -v multica-models:/models \
  ghcr.io/firtal-group/multica-inference:dev
```

The entrypoint pre-fetches the plapre weights into `/models` (mounted as a
volume so restarts don't re-download). Mock mode and `INFERENCE_SKIP_PREFETCH`
both bypass the pull.

## Voices

`syvai/plapre-nano` ships with five built-in voices, exposed verbatim by
`GET /plapre/voices`:

| voice_id | label | gender |
|----------|-------|--------|
| `ida` | Ida | f |
| `liv` | Liv | f |
| `tor` | Tor | m |
| `ask` | Ask | m |
| `kaj` | Kaj | m |

Plapre also has a `mic` slot for voice-cloning from a reference clip; we
deliberately don't expose it via `GET /plapre/voices` so the synthesize
endpoint can't be used as a cloning service. If we ship a curated voice-clone
flow later, it lives behind a separate authenticated endpoint.

## Environment variables

See `.env.example`. The two that always matter:

- `INFERENCE_API_KEY` — bearer secret the multica backend uses. The container
  refuses to boot without one unless `INFERENCE_MOCK_MODE=1`.
- `HF_TOKEN` — Hugging Face token with access to gated `syvai/plapre-nano`.
  Pinged Jesper for org access via Multica's HF account; until then, dev runs
  use mock mode.

`PLAPRE_GPU_MEMORY_UTILIZATION` (default `0.30`) caps plapre's vLLM share of
VRAM. On the deploy target — Hetzner GEX44 (RTX 4000 Ada, 20 GB) — `0.30`
reserves ~6 GB for plapre and leaves ~14 GB for hviske + activations.

## GPU memory budget

Deploy target is **Hetzner GEX44 (RTX 4000 Ada, 20 GB)** per the JEH-729 GPU
spike — Sliplane has no GPU offering, so the L4/A10 plan is replaced.

| Component | fp16 | int8 |
|-----------|------|------|
| plapre-nano weights (327M params) | ~0.6 GB | ~0.3 GB |
| Kanade vocoder | ~0.4 GB | ~0.4 GB |
| vLLM KV cache + activations | ~1–2 GB | ~1–2 GB |
| hviske-v3 large-v3 (faster-whisper) | ~3 GB | ~1.5–2 GB |
| Faster-whisper activations | ~0.5 GB | ~0.5 GB |
| PyTorch CUDA workspace | ~1.5 GB | ~1 GB |
| **Combined total** | **~7–8 GB** | **~5–6 GB** |

20 GB leaves ~12–13 GB headroom for batching or future models. Verified at
the estimate level; first deploy validates against real hardware.

## Architecture decisions

- **Single FastAPI process.** Plapre's `Plapre.speak()` and faster-whisper's
  inference run in the same Python process so they share the torch CUDA
  context and avoid double model-load on restart. We deliberately skip
  proxying to plapre's own `plapre-serve` subprocess — the IPC layer would
  add ops surface and break shared CUDA state.
- **Mock mode is first-class.** The frontend and backend proxy work need a
  reachable endpoint that returns the right shape; mock mode avoids
  serializing dev work behind GPU access.
- **Bearer auth, not network ACL.** Inference is auth-gated at the app layer
  so the same image works on a public port for one-off testing without
  changing config.
- **`audio/L16; rate=24000`** is the correct media type for raw PCM over HTTP
  (RFC 2586). 24 kHz / s16-le matches plapre's native output and is
  decodable directly by Web Audio API in slice C.
- **TTFA caveat.** `Plapre.speak()` is synchronous — first byte = full
  inference time, not chunked-while-generating. If we miss the <500 ms TTFA
  target, the slice B proxy splits text into sentences and pipes them through
  `speak()` per-sentence to recover per-clause streaming. Subprocess proxy to
  `plapre-serve` is a slice F decision if even that isn't enough.

## Open verification items (slice A → deploy)

1. **HF access** — pinged Jesper for org access to `syvai/plapre-nano`.
   Container boots in mock mode until `HF_TOKEN` is on Sliplane.
2. **`Plapre.speak()` API stability** — pinned to `git+https://github.com/syv-ai/plapre.git@main`
   in the Dockerfile. Pin to a specific commit before the first real-mode
   deploy.
3. **TTFA on real model** — measured at first deploy. If it exceeds ~500 ms,
   slice B introduces sentence-level chunking in the proxy (see Architecture
   decisions above).
4. **Combined GPU memory on RTX 4000 Ada** — table above is estimate; first
   deploy measures.

## Slice A scope (this PR) vs follow-ups

| Slice | Scope | Status |
|-------|-------|--------|
| A (JEH-740) | container shape, `/plapre/synthesize`, mock mode, deploy docs | this PR |
| 4 (JEH-729) | `/hviske/transcribe` real implementation, GPU spike | separate PR, parallel |
| B (JEH-740) | backend proxy + voice-summary LLM hop | blocked on JEH-729 dictation MVP |
| F (JEH-740) | A/B test page (Plapre vs ElevenLabs Flash v2.5) | blocked on slice B |
