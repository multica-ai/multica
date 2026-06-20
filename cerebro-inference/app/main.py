import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.config import get_settings

log = logging.getLogger("multica.inference")


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    logging.basicConfig(level=settings.log_level.upper())

    if not settings.inference_api_key and not settings.inference_mock_mode:
        # Refuse to boot. An open inference endpoint exposes GPU + content-policy
        # surface; require an explicit opt-out via mock mode for dev/CI.
        raise RuntimeError(
            "INFERENCE_API_KEY is required unless INFERENCE_MOCK_MODE=1"
        )

    app.state.plapre = None
    app.state.hviske = None

    # Import each model lazily so a single-surface deploy (e.g. hviske-only for
    # tale→tekst) doesn't pull the other model's heavy deps into the image.
    if settings.inference_enable_plapre:
        from app.plapre.model import PlapreModel

        app.state.plapre = PlapreModel.load(settings)
        log.info("plapre ready (mock=%s)", settings.inference_mock_mode)

    if settings.inference_enable_hviske:
        from app.hviske.model import HviskeModel

        app.state.hviske = HviskeModel.load(settings)
        log.info("hviske ready (mock=%s)", settings.inference_mock_mode)

    try:
        yield
    finally:
        if app.state.plapre is not None:
            app.state.plapre.close()
        if app.state.hviske is not None:
            app.state.hviske.close()


app = FastAPI(
    title="Multica inference",
    description="TTS (plapre-nano) + STT (hviske-v3) for the cerebro voice extension.",
    version="0.1.0",
    lifespan=lifespan,
)

# Router inclusion mirrors the model toggles so a disabled surface exposes no
# routes at all (rather than routes that 500 on a None model handle).
_settings = get_settings()
if _settings.inference_enable_plapre:
    from app.plapre.router import router as plapre_router

    app.include_router(plapre_router)
if _settings.inference_enable_hviske:
    from app.hviske.router import router as hviske_router

    app.include_router(hviske_router)


@app.get("/healthz", tags=["health"])
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/readyz", tags=["health"])
async def readyz() -> dict[str, object]:
    plapre = app.state.plapre
    hviske = app.state.hviske
    plapre_ready = bool(plapre is not None and plapre.ready)
    hviske_ready = bool(hviske is not None and hviske.ready)
    return {
        "status": "ok" if (plapre_ready or hviske_ready) else "loading",
        "plapre": plapre_ready,
        "hviske": hviske_ready,
    }
