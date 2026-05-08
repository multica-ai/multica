from __future__ import annotations

import hashlib
import logging
import math
import struct
from abc import ABC, abstractmethod
from collections.abc import Iterator

from app.config import Settings
from app.plapre.voices import VOICES_BY_ID

log = logging.getLogger("multica.inference.plapre")


class UnknownVoiceError(ValueError):
    pass


class PlapreModel(ABC):
    sample_rate: int
    chunk_ms: int
    default_voice: str

    @property
    @abstractmethod
    def ready(self) -> bool: ...

    @abstractmethod
    def synthesize_stream(self, text: str, voice_id: str | None) -> Iterator[bytes]:
        """Yield PCM s16-le chunks at `sample_rate` Hz, `chunk_ms` ms each."""

    def close(self) -> None:
        return None

    @staticmethod
    def load(settings: Settings) -> PlapreModel:
        if settings.inference_mock_mode:
            return MockPlapreModel(settings)
        return RealPlapreModel(settings)

    def resolve_voice(self, voice_id: str | None) -> str:
        v = voice_id or self.default_voice
        if v not in VOICES_BY_ID:
            raise UnknownVoiceError(v)
        return v


class MockPlapreModel(PlapreModel):
    """Sine-wave stand-in for real plapre. Used in CI, frontend integration
    without a GPU, and end-to-end protocol tests. Each voice gets a different
    fundamental frequency so listeners can confirm the voice_id round-trips."""

    def __init__(self, settings: Settings) -> None:
        self.sample_rate = settings.plapre_sample_rate
        self.chunk_ms = settings.plapre_chunk_ms
        self.default_voice = settings.plapre_default_voice

    @property
    def ready(self) -> bool:
        return True

    def synthesize_stream(self, text: str, voice_id: str | None) -> Iterator[bytes]:
        voice = self.resolve_voice(voice_id)

        samples_per_chunk = int(self.sample_rate * self.chunk_ms / 1000)
        # ~80ms per char is enough to convey duration without being slow in tests.
        # Cap at 30s so a runaway long string doesn't blow up the buffer.
        total_ms = max(self.chunk_ms, min(len(text) * 80, 30_000))
        total_samples = int(self.sample_rate * total_ms / 1000)

        # Map voice_id deterministically to a frequency between 180-360 Hz so
        # different voices are audibly distinct under headphones. md5 keeps the
        # mapping stable across processes — built-in hash() is salted and
        # changes between runs.
        freq = 180.0 + (hashlib.md5(voice.encode()).digest()[0] % 180)
        amp = 0.20  # Conservative — peak around -14 dBFS.

        frame = 0
        while frame < total_samples:
            buf = bytearray()
            for _ in range(samples_per_chunk):
                if frame >= total_samples:
                    break
                t = frame / self.sample_rate
                # Linear fade-in/out over 20ms each end to avoid clicks.
                fade_samples = int(self.sample_rate * 0.02)
                env = 1.0
                if frame < fade_samples:
                    env = frame / fade_samples
                elif total_samples - frame < fade_samples:
                    env = (total_samples - frame) / fade_samples
                sample = int(amp * env * 32767 * math.sin(2 * math.pi * freq * t))
                buf += struct.pack("<h", sample)
                frame += 1
            yield bytes(buf)


class RealPlapreModel(PlapreModel):
    """plapre-nano via the upstream `plapre` Python API.

    Plapre is a 327M-param autoregressive LM (vLLM-served) + Kanade vocoder.
    `Plapre.speak()` returns a 24 kHz float32 numpy array; we convert to s16-le
    PCM and chunk it for the streaming response.

    Note on TTFA: the Python API is synchronous, so first-byte latency = full
    inference time. Per-sentence streaming (lower TTFA) only kicks in if we
    proxy to plapre's own `plapre-serve`, which is a slice B/F decision; we
    deliberately stay with the in-process API here for shared CUDA context
    with hviske."""

    def __init__(self, settings: Settings) -> None:
        self.sample_rate = settings.plapre_sample_rate
        self.chunk_ms = settings.plapre_chunk_ms
        self.default_voice = settings.plapre_default_voice
        self.settings = settings
        self._plapre = None
        self._load()

    def _load(self) -> None:
        from plapre import Plapre

        self._plapre = Plapre(
            self.settings.plapre_model_id,
            gpu_memory_utilization=self.settings.plapre_gpu_memory_utilization,
        )
        log.info(
            "plapre loaded model=%s gpu_memory_utilization=%s",
            self.settings.plapre_model_id,
            self.settings.plapre_gpu_memory_utilization,
        )

    @property
    def ready(self) -> bool:
        return self._plapre is not None

    def synthesize_stream(self, text: str, voice_id: str | None) -> Iterator[bytes]:
        import numpy as np

        assert self._plapre is not None
        voice = self.resolve_voice(voice_id)

        # plapre.Plapre.speak returns float32 24 kHz mono. Defaults for
        # temperature / top_p / top_k / max_tokens are set inside plapre and
        # tune-able per-deploy, not per-request — we don't surface them here.
        waveform = self._plapre.speak(text, speaker=voice)
        waveform = np.clip(np.asarray(waveform, dtype=np.float32), -1.0, 1.0)
        pcm = (waveform * 32767).astype("<i2").tobytes()

        samples_per_chunk = int(self.sample_rate * self.chunk_ms / 1000)
        bytes_per_chunk = samples_per_chunk * 2
        for i in range(0, len(pcm), bytes_per_chunk):
            yield pcm[i : i + bytes_per_chunk]

    def close(self) -> None:
        # Drop the reference so vLLM's cleanup runs at GC.
        self._plapre = None
