from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="", env_file=".env", extra="ignore")

    inference_api_key: str = Field(default="", description="Bearer token the backend sends.")
    hf_token: str = Field(default="", description="HF token for gated model pulls.")
    inference_skip_prefetch: bool = False
    inference_mock_mode: bool = False

    # Independent model toggles (JEH-729 slice 4). A tale→tekst-only deploy runs
    # hviske alone and skips plapre entirely, so the image needs neither the
    # plapre git package nor vLLM GPU memory. Both default on for the combined
    # container.
    inference_enable_plapre: bool = True
    inference_enable_hviske: bool = True

    plapre_model_id: str = "syvai/plapre-nano"
    plapre_default_voice: str = "ida"
    plapre_sample_rate: int = 24000
    plapre_chunk_ms: int = 40
    # Slice cap of plapre's vLLM-managed VRAM. Hetzner GEX44 (RTX 4000 Ada,
    # 20 GB) needs ~5–6 GB for plapre to leave room for hviske + activations.
    # 0.30 = ~6 GB. Adjust per GPU during deploy.
    plapre_gpu_memory_utilization: float = 0.30

    hviske_model_id: str = "syvai/hviske-v3-conversation"
    # ISO-639-1 language hint passed to Whisper decoding. Danish for hviske.
    hviske_language: str = "da"
    # Long-form chunking window (seconds) for the ASR pipeline.
    hviske_chunk_length_s: int = 30

    # FIR-1797 — optional LLM cleanup of the raw transcript (punctuation,
    # capitalisation, paragraph breaks; the "Wispr Flow"-style polish). All AI
    # goes through the Firtal AI gateway (the OpenAI-compatible proxy the cerebro
    # server runtime also uses) — never a provider API directly. The backend
    # forwards a per-request `cleanup` flag from the user's dictation setting;
    # cleanup only runs when that flag is set AND the gateway URL + key are
    # configured here, so it degrades to a no-op (returns the raw text) when
    # unconfigured. `hviske_cleanup_enabled` is a global kill-switch.
    hviske_cleanup_enabled: bool = True
    firtal_ai_gateway_url: str = Field(
        default="", description="Firtal AI gateway base URL (proxy path is appended)."
    )
    firtal_ai_gateway_key: str = Field(
        default="", description="Firtal AI gateway API key (Bearer)."
    )
    # Must be a model the Firtal AI gateway has registered + active. The EU
    # variant keeps the cleanup text path in-region (GDPR), matching the
    # Frankfurt audio path. `claude-haiku-4-5-20251001` (no eu. prefix) is NOT
    # registered in the gateway and returns 403.
    hviske_cleanup_model: str = "eu.anthropic.claude-haiku-4-5-20251001-v1:0"

    log_level: str = "INFO"


_settings: Settings | None = None


def get_settings() -> Settings:
    global _settings
    if _settings is None:
        _settings = Settings()
    return _settings
