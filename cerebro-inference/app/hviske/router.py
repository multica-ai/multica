from fastapi import APIRouter, Depends, HTTPException, status

from app.auth import require_bearer

# JEH-729 slice 4 fills in the real /transcribe handler (faster-whisper +
# hviske-v3-conversation). This stub exists so the inference container has a
# stable shape — the multica backend can already wire up routes against
# /hviske/transcribe and get a clear 503 until the model is added.

router = APIRouter(prefix="/hviske", tags=["hviske"])


@router.post("/transcribe", dependencies=[Depends(require_bearer)])
async def transcribe() -> None:
    raise HTTPException(
        status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
        detail="hviske not implemented in this image (see JEH-729 slice 4)",
    )
