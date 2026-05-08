from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from app.auth import require_bearer
from app.plapre.model import PlapreModel, UnknownVoiceError
from app.plapre.voices import VOICES

router = APIRouter(prefix="/plapre", tags=["plapre"])


class SynthesizeRequest(BaseModel):
    text: str = Field(min_length=1, max_length=4000)
    voice_id: str | None = None


@router.get("/voices", dependencies=[Depends(require_bearer)])
async def list_voices() -> dict[str, list[dict[str, str]]]:
    return {
        "voices": [
            {"voice_id": v.voice_id, "label": v.label, "gender": v.gender} for v in VOICES
        ]
    }


@router.post("/synthesize", dependencies=[Depends(require_bearer)])
async def synthesize(req: SynthesizeRequest, request: Request) -> StreamingResponse:
    plapre: PlapreModel = request.app.state.plapre

    # Validate up-front so a bad voice_id returns 400 before the response
    # starts streaming. After headers are sent FastAPI can't surface a JSON
    # error anymore, so the generator below assumes a resolved voice.
    try:
        resolved = plapre.resolve_voice(req.voice_id)
    except UnknownVoiceError as e:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"unknown voice_id: {e}",
        ) from e

    return StreamingResponse(
        plapre.synthesize_stream(req.text, resolved),
        media_type="audio/L16; rate=24000",
        headers={
            "X-Plapre-Sample-Rate": str(plapre.sample_rate),
            "X-Plapre-Encoding": "pcm_s16le",
            "X-Plapre-Voice-Id": resolved,
            "Cache-Control": "no-store",
        },
    )
