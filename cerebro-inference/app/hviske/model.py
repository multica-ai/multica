from __future__ import annotations

import logging
import os
import tempfile
from abc import ABC, abstractmethod

from app.config import Settings

log = logging.getLogger("multica.inference.hviske")


class HviskeModel(ABC):
    """Danish speech-to-text via hviske-v3-conversation (a Whisper large-v3
    fine-tune). The container loads one of these at startup and serves
    `/hviske/transcribe`."""

    default_language: str

    @property
    @abstractmethod
    def ready(self) -> bool: ...

    @abstractmethod
    def transcribe(
        self,
        audio_bytes: bytes,
        language: str | None = None,
        glossary: str | None = None,
    ) -> str:
        """Decode `audio_bytes` (any ffmpeg-readable container) and return the
        transcribed text. `glossary` is an optional list of domain terms (comma
        separated) used to bias decoding toward proper nouns / brand names."""

    def close(self) -> None:
        return None

    @staticmethod
    def load(settings: Settings) -> HviskeModel:
        if settings.inference_mock_mode:
            return MockHviskeModel(settings)
        return RealHviskeModel(settings)


class MockHviskeModel(HviskeModel):
    """No-GPU stand-in so the frontend + backend proxy can integrate against a
    stable shape in dev/CI. Echoes a fixed marker plus the byte count so a
    round-trip is observable without a model."""

    def __init__(self, settings: Settings) -> None:
        self.default_language = settings.hviske_language

    @property
    def ready(self) -> bool:
        return True

    def transcribe(
        self,
        audio_bytes: bytes,
        language: str | None = None,
        glossary: str | None = None,
    ) -> str:
        suffix = " +glossary" if glossary else ""
        return f"(mock transcription, {len(audio_bytes)} bytes{suffix})"


class RealHviskeModel(HviskeModel):
    """hviske-v3-conversation via the transformers ASR pipeline.

    The pipeline accepts a file path and decodes any container (webm/opus from
    the browser MediaRecorder, wav, mp3, …) through ffmpeg, so we write the
    upload to a temp file rather than decoding ourselves. First-byte latency is
    full-utterance (Whisper is not streaming); chunk_length_s handles long-form
    clips by windowing."""

    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.default_language = settings.hviske_language
        self._pipe = None
        self._load()

    def _load(self) -> None:
        import torch
        from transformers import pipeline

        if torch.cuda.is_available():
            device, dtype = "cuda", torch.float16
        elif getattr(torch.backends, "mps", None) and torch.backends.mps.is_available():
            device, dtype = "mps", torch.float16
        else:
            device, dtype = "cpu", torch.float32

        self._pipe = pipeline(
            "automatic-speech-recognition",
            model=self.settings.hviske_model_id,
            token=self.settings.hf_token or None,
            device=device,
            dtype=dtype,
        )
        log.info(
            "hviske loaded model=%s device=%s",
            self.settings.hviske_model_id,
            device,
        )

    @property
    def ready(self) -> bool:
        return self._pipe is not None

    def transcribe(
        self,
        audio_bytes: bytes,
        language: str | None = None,
        glossary: str | None = None,
    ) -> str:
        assert self._pipe is not None
        generate_kwargs = {
            "language": language or self.default_language,
            "task": "transcribe",
        }
        prompt_ids = self._build_prompt_ids(glossary)
        if prompt_ids is not None:
            generate_kwargs["prompt_ids"] = prompt_ids
        with tempfile.NamedTemporaryFile(suffix=".audio", delete=False) as f:
            f.write(audio_bytes)
            path = f.name
        try:
            try:
                out = self._pipe(
                    path,
                    generate_kwargs=generate_kwargs,
                    chunk_length_s=self.settings.hviske_chunk_length_s,
                )
            except Exception:
                # Glossary biasing (prompt_ids) is the only optional input, and
                # Whisper's prompt+long-form-chunking combo is brittle across
                # transformers versions. Never let it break a transcription:
                # drop the bias and retry the proven path once.
                if "prompt_ids" not in generate_kwargs:
                    raise
                log.warning(
                    "hviske transcribe with glossary failed; retrying unbiased",
                    exc_info=True,
                )
                generate_kwargs.pop("prompt_ids", None)
                out = self._pipe(
                    path,
                    generate_kwargs=generate_kwargs,
                    chunk_length_s=self.settings.hviske_chunk_length_s,
                )
        finally:
            os.unlink(path)
        return (out.get("text") or "").strip()

    def _build_prompt_ids(self, glossary: str | None):
        """Turn a glossary string into Whisper `prompt_ids` that bias decoding
        toward those terms. Returns None (no bias) for an empty glossary or if
        the tokenizer cannot build the prompt — transcription must never depend
        on this succeeding."""
        if not glossary or not glossary.strip():
            return None
        try:
            tokenizer = self._pipe.tokenizer
            prompt_ids = tokenizer.get_prompt_ids(glossary.strip(), return_tensors="pt")
            device = getattr(self._pipe.model, "device", None)
            if device is not None:
                prompt_ids = prompt_ids.to(device)
            return prompt_ids
        except Exception:
            log.warning("hviske glossary prompt build failed; ignoring", exc_info=True)
            return None

    def close(self) -> None:
        # Drop the reference so torch frees the weights at GC.
        self._pipe = None
