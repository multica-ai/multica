import hmac

from fastapi import Header, HTTPException, status

from app.config import get_settings


async def require_bearer(authorization: str | None = Header(default=None)) -> None:
    """Reject requests without a matching `Authorization: Bearer <INFERENCE_API_KEY>`.

    Empty `INFERENCE_API_KEY` disables auth — only allowed when mock mode is on,
    so dev/CI doesn't need to manage a secret. In every other case the container
    refuses to start without a key (see app.main.lifespan)."""
    settings = get_settings()
    if not settings.inference_api_key and settings.inference_mock_mode:
        return

    if not authorization or not authorization.lower().startswith("bearer "):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="missing bearer token",
            headers={"WWW-Authenticate": "Bearer"},
        )

    presented = authorization[len("Bearer ") :].strip()
    if not hmac.compare_digest(presented, settings.inference_api_key):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="invalid bearer token",
            headers={"WWW-Authenticate": "Bearer"},
        )
