"""Tests for the best-effort transcript cleanup (FIR-1797).

Cleanup must never make a successful transcription worse: a misbehaving model
(refusal, answering the transcript as if it were a prompt) returns commentary
far longer than the input, and we keep the raw transcript instead.
"""

from __future__ import annotations

import json
from contextlib import contextmanager
from unittest.mock import patch

from app.config import Settings
from app.hviske import cleanup as cleanup_mod
from app.hviske.cleanup import _select_relevant_terms, cleanup_transcript

RAW = "bestil dafni og simon levelt fra helsebixen og send en påmindelse i morgen"


def _settings(**over) -> Settings:
    base = dict(
        hviske_cleanup_enabled=True,
        firtal_ai_gateway_url="https://gateway.example",
        firtal_ai_gateway_key="k",
    )
    base.update(over)
    return Settings(**base)


@contextmanager
def _gateway_returns(content: str):
    """Patch urlopen so the gateway responds with `content` as the message."""
    body = json.dumps({"choices": [{"message": {"content": content}}]}).encode()

    class _Resp:
        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def read(self):
            return body

    with patch.object(cleanup_mod.urllib.request, "urlopen", return_value=_Resp()):
        yield


def test_normal_cleanup_is_returned():
    polished = "Bestil Dafni og Simon Levelt fra Helsebixen og send en påmindelse i morgen."
    with _gateway_returns(polished):
        assert cleanup_transcript(_settings(), RAW) == polished


def test_glossary_correction_runs_when_format_cleanup_is_off():
    corrected = "bestil Dafni og Simon Levelt fra Helsebixen og send en påmindelse i morgen"
    with _gateway_returns(corrected):
        assert (
            cleanup_transcript(
                _settings(),
                RAW,
                glossary="Dafni, Simon Levelt, Helsebixen",
                format_text=False,
            )
            == corrected
        )


def test_glossary_correction_rejects_words_not_in_transcript_or_glossary():
    invented = "bestil Dafni og Simon Levelt fra Helsebixen gratis i morgen"
    with _gateway_returns(invented):
        assert (
            cleanup_transcript(
                _settings(),
                RAW,
                glossary="Dafni, Simon Levelt, Helsebixen",
                format_text=False,
            )
            == RAW
        )


def test_only_glossary_terms_resembling_the_transcript_are_checked():
    selected = _select_relevant_terms(
        "bestil noget fra helse biksen i morgen",
        ["Helsebixen", "made4men", "Dafni", "Simon Levelt"],
    )

    assert selected == ["Helsebixen"]


def test_refusal_or_rambling_is_discarded():
    # The model ignored the prompt and answered/refused — output balloons.
    refusal = (
        "I'm not able to clean this text as requested because it appears to be a "
        "command or instruction rather than a raw speech-to-text transcript. The "
        "text is already well-formatted. If you have a raw transcript, please "
        "provide it and I'll be happy to help."
    )
    with _gateway_returns(refusal):
        assert cleanup_transcript(_settings(), RAW) == RAW


def test_disabled_returns_raw():
    assert cleanup_transcript(_settings(hviske_cleanup_enabled=False), RAW) == RAW


def test_unconfigured_gateway_returns_raw():
    assert cleanup_transcript(_settings(firtal_ai_gateway_key=""), RAW) == RAW


def test_network_failure_returns_raw():
    with patch.object(
        cleanup_mod.urllib.request,
        "urlopen",
        side_effect=cleanup_mod.urllib.error.URLError("boom"),
    ):
        assert cleanup_transcript(_settings(), RAW) == RAW
