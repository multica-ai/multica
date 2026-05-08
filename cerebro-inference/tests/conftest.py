import os

import pytest
from fastapi.testclient import TestClient

# Force mock mode + a known API key BEFORE app import so Settings picks them up.
os.environ.setdefault("INFERENCE_MOCK_MODE", "1")
os.environ.setdefault("INFERENCE_API_KEY", "test-secret-32-bytes-12345678901234")

from app.config import get_settings  # noqa: E402
from app.main import app  # noqa: E402


@pytest.fixture(autouse=True)
def _reset_settings():
    # Settings is a process-wide singleton; reset after each test so env-var
    # tweaks don't leak.
    import app.config

    app.config._settings = None
    yield
    app.config._settings = None


@pytest.fixture
def client():
    # Context-manager form is required for Starlette to fire lifespan, which is
    # what loads the plapre model into app.state.
    with TestClient(app) as c:
        yield c


@pytest.fixture
def auth_headers() -> dict[str, str]:
    return {"Authorization": f"Bearer {get_settings().inference_api_key}"}
