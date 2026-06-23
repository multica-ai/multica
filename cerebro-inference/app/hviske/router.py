import asyncio

from fastapi import APIRouter, Depends, File, Form, HTTPException, Request, UploadFile, status

from app.auth import require_bearer
from app.config import get_settings
from app.hviske.cleanup import cleanup_transcript
from app.hviske.model import HviskeModel

router = APIRouter(prefix="/hviske", tags=["hviske"])

# 25 MB cap. A dictation utterance is seconds of Opus/WebM — kilobytes — so this
# only guards against a runaway upload, never a legitimate clip.
MAX_AUDIO_BYTES = 25 * 1024 * 1024


@router.post("/transcribe", dependencies=[Depends(require_bearer)])
async def transcribe(
    request: Request,
    file: UploadFile = File(...),
    language: str | None = Form(default=None),
    # FIR-1797: optional comma-separated domain terms to bias decoding, and an
    # opt-in flag to run the LLM punctuation/structure cleanup pass.
    glossary: str | None = Form(default=None),
    cleanup: bool = Form(default=False),
) -> dict[str, str]:
    hviske: HviskeModel = request.app.state.hviske
    if hviske is None or not hviske.ready:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="hviske not ready",
        )

    audio = await file.read()
    if not audio:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="empty audio upload",
        )
    if len(audio) > MAX_AUDIO_BYTES:
        raise HTTPException(
            status_code=status.HTTP_413_REQUEST_ENTITY_TOO_LARGE,
            detail=f"audio exceeds {MAX_AUDIO_BYTES} bytes",
        )

    text = hviske.transcribe(audio, language=language, glossary=glossary)
    if cleanup and text.strip():
        # Off the event loop: the cleanup call is a blocking stdlib HTTP request.
        text = await asyncio.to_thread(cleanup_transcript, get_settings(), text)
    return {"text": text, "language": language or hviske.default_language}
