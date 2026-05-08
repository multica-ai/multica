from dataclasses import dataclass


@dataclass(frozen=True)
class Voice:
    voice_id: str
    label: str
    gender: str  # "f" | "m" | "n"


# The five built-in plapre-nano voices, as enumerated by the model card. Order
# here is the order surfaced by GET /plapre/voices and rendered directly by
# the frontend (slice C). `mic` is plapre's voice-cloning slot and is
# intentionally omitted — exposing it would let any caller supply a reference
# clip via this endpoint.
VOICES: list[Voice] = [
    Voice("ida", "Ida", "f"),
    Voice("liv", "Liv", "f"),
    Voice("tor", "Tor", "m"),
    Voice("ask", "Ask", "m"),
    Voice("kaj", "Kaj", "m"),
]

VOICES_BY_ID: dict[str, Voice] = {v.voice_id: v for v in VOICES}
