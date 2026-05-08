import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.config import get_settings
from app.hviske.router import router as hviske_router
from app.plapre.model import PlapreModel
from app.plapre.router import router as plapre_router

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

    plapre = PlapreModel.load(settings)
    app.state.plapre = plapre
    log.info("plapre ready (mock=%s, voice=%s)", settings.inference_mock_mode, plapre.default_voice)

    try:
        yield
    finally:
        plapre.close()


app = FastAPI(
    title="Multica inference",
    description="TTS (plapre-nano) + STT (hviske-v3, JEH-729 slice 4) for the dictation extension.",
    version="0.1.0",
    lifespan=lifespan,
)

app.include_router(plapre_router)
app.include_router(hviske_router)


@app.get("/healthz", tags=["health"])
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/readyz", tags=["health"])
async def readyz() -> dict[str, object]:
    plapre: PlapreModel = app.state.plapre
    return {
        "status": "ok" if plapre.ready else "loading",
        "plapre": plapre.ready,
        "hviske": False,  # Filled in by JEH-729 slice 4.
    }
