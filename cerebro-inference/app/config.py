from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="", env_file=".env", extra="ignore")

    inference_api_key: str = Field(default="", description="Bearer token the backend sends.")
    hf_token: str = Field(default="", description="HF token for gated model pulls.")
    inference_skip_prefetch: bool = False
    inference_mock_mode: bool = False

    plapre_model_id: str = "syvai/plapre-nano"
    plapre_default_voice: str = "ida"
    plapre_sample_rate: int = 24000
    plapre_chunk_ms: int = 40
    # Slice cap of plapre's vLLM-managed VRAM. Hetzner GEX44 (RTX 4000 Ada,
    # 20 GB) needs ~5–6 GB for plapre to leave room for hviske + activations.
    # 0.30 = ~6 GB. Adjust per GPU during deploy.
    plapre_gpu_memory_utilization: float = 0.30

    hviske_model_id: str = "syvai/hviske-v3-conversation"

    log_level: str = "INFO"


_settings: Settings | None = None


def get_settings() -> Settings:
    global _settings
    if _settings is None:
        _settings = Settings()
    return _settings
