"""Optional LLM cleanup of a raw transcript (FIR-1797).

The "Wispr Flow"-style polish: take the raw Whisper output and fix punctuation,
capitalisation and paragraph breaks without changing the words.

All AI goes through the **Firtal AI gateway** — the OpenAI-compatible proxy the
cerebro server runtime uses (`{base}/api/ai/proxy/v1/chat/completions`, Bearer
auth) — never a provider API directly. The call is a small synchronous stdlib
urllib request (no extra dependency in the inference image); the router runs it
off the event loop via `asyncio.to_thread`.

It is best-effort by design: any failure (no gateway configured, network, bad
response) returns the original text unchanged, so cleanup can never turn a
successful transcription into a failed dictation.
"""

from __future__ import annotations

import json
import logging
import urllib.error
import urllib.request

from app.config import Settings

log = logging.getLogger("multica.inference.hviske.cleanup")

# OpenAI-compatible chat-completions path on the Firtal AI gateway, matching the
# cerebro server runtime's GatewayClient.
_PROXY_PATH = "/api/ai/proxy/v1/chat/completions"
_TIMEOUT_S = 8.0

# Words-only polish: the model must not paraphrase, translate, answer, or add
# anything — only repair the mechanics of the dictated text.
_SYSTEM_PROMPT = (
    "You clean up raw speech-to-text transcripts. Fix punctuation, "
    "capitalisation, obvious mis-spacing and paragraph breaks so the text reads "
    "well. Do NOT translate, paraphrase, summarise, answer, or add or remove "
    "information — keep the speaker's exact words and language. Return ONLY the "
    "cleaned text, with no preamble, quotes, or commentary."
)


def cleanup_transcript(settings: Settings, text: str) -> str:
    """Return a punctuation/structure-cleaned version of `text`, or `text`
    unchanged if cleanup is disabled, the gateway is unconfigured, or it fails."""
    if not settings.hviske_cleanup_enabled:
        return text
    base = (settings.firtal_ai_gateway_url or "").rstrip("/")
    if not base or not settings.firtal_ai_gateway_key:
        log.debug("transcript cleanup requested but Firtal AI gateway is not configured")
        return text
    stripped = text.strip()
    if not stripped:
        return text

    payload = {
        "model": settings.hviske_cleanup_model,
        "max_tokens": min(2048, len(stripped) // 2 + 256),
        "stream": False,
        "messages": [
            {"role": "system", "content": _SYSTEM_PROMPT},
            {"role": "user", "content": stripped},
        ],
    }
    req = urllib.request.Request(
        base + _PROXY_PATH,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "content-type": "application/json",
            "authorization": f"Bearer {settings.firtal_ai_gateway_key}",
            "x-skill": "cerebro-dictation-cleanup",
            "x-tags": "multica,cerebro,dictation",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=_TIMEOUT_S) as resp:
            body = json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, TimeoutError, ValueError) as err:
        log.warning("transcript cleanup failed; returning raw text: %s", err)
        return text

    # OpenAI-compatible: {"choices": [{"message": {"content": "..."}}]}.
    cleaned = _extract_content(body)
    return cleaned or text


def _extract_content(body: object) -> str:
    if not isinstance(body, dict):
        return ""
    choices = body.get("choices")
    if not isinstance(choices, list) or not choices:
        return ""
    first = choices[0]
    if not isinstance(first, dict):
        return ""
    message = first.get("message")
    if not isinstance(message, dict):
        return ""
    content = message.get("content")
    return content.strip() if isinstance(content, str) else ""
