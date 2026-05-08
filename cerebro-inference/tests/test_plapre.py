SAMPLE_RATE = 24000
BYTES_PER_SAMPLE = 2  # s16-le


def test_synthesize_streams_pcm_in_mock_mode(client, auth_headers):
    with client.stream(
        "POST",
        "/plapre/synthesize",
        json={"text": "Hej, dette er en test."},
        headers=auth_headers,
    ) as r:
        assert r.status_code == 200
        assert r.headers["X-Plapre-Sample-Rate"] == str(SAMPLE_RATE)
        assert r.headers["X-Plapre-Encoding"] == "pcm_s16le"
        assert r.headers["X-Plapre-Voice-Id"] == "ida"
        assert r.headers["content-type"].startswith("audio/L16")

        body = b"".join(r.iter_bytes())

    # s16-le → byte count must be even.
    assert len(body) % BYTES_PER_SAMPLE == 0

    # 22 chars * 80ms = 1760ms minimum at 24kHz s16 → ~84KB. Verify we got
    # something in that ballpark (and not zero, and not absurd).
    expected_min_bytes = SAMPLE_RATE * BYTES_PER_SAMPLE * 1  # >= 1s
    expected_max_bytes = SAMPLE_RATE * BYTES_PER_SAMPLE * 31  # <= 31s
    assert expected_min_bytes <= len(body) <= expected_max_bytes


def test_model_yields_chunks_of_chunk_ms(client):
    # Test the underlying generator directly. Asserting per-chunk size through
    # HTTP is unreliable because TCP/httpx coalesces small writes.
    from app.plapre.model import PlapreModel

    plapre: PlapreModel = client.app.state.plapre
    chunks = list(plapre.synthesize_stream("ABCDEFGHIJ", "ida"))

    full_chunk_bytes = int(SAMPLE_RATE * plapre.chunk_ms / 1000) * BYTES_PER_SAMPLE
    assert chunks, "expected at least one chunk"
    for c in chunks[:-1]:
        assert len(c) == full_chunk_bytes, f"unexpected chunk size {len(c)}"
    assert 0 < len(chunks[-1]) <= full_chunk_bytes


def test_synthesize_uses_specified_voice(client, auth_headers):
    r = client.post(
        "/plapre/synthesize",
        json={"text": "hej", "voice_id": "tor"},
        headers=auth_headers,
    )
    assert r.status_code == 200
    assert r.headers["X-Plapre-Voice-Id"] == "tor"


def test_synthesize_rejects_unknown_voice(client, auth_headers):
    r = client.post(
        "/plapre/synthesize",
        json={"text": "hej", "voice_id": "does-not-exist"},
        headers=auth_headers,
    )
    assert r.status_code == 400
    assert "unknown voice_id" in r.json()["detail"]


def test_synthesize_rejects_empty_text(client, auth_headers):
    r = client.post(
        "/plapre/synthesize",
        json={"text": ""},
        headers=auth_headers,
    )
    assert r.status_code == 422


def test_synthesize_rejects_oversized_text(client, auth_headers):
    r = client.post(
        "/plapre/synthesize",
        json={"text": "x" * 5000},
        headers=auth_headers,
    )
    assert r.status_code == 422


def test_different_voices_produce_different_audio(client, auth_headers):
    """Mock mode maps voice_id to a different sine frequency. The protocol
    contract is that voice_id round-trips through to the audio output, so a
    smoke test that two voices produce different bytes catches accidental
    voice-id-ignored regressions."""
    text = "hej hej hej"
    bodies = {}
    for voice in ("ida", "tor"):
        r = client.post(
            "/plapre/synthesize",
            json={"text": text, "voice_id": voice},
            headers=auth_headers,
        )
        assert r.status_code == 200
        bodies[voice] = r.content
    assert bodies["ida"] != bodies["tor"]
