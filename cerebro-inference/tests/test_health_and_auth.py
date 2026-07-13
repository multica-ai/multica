def test_healthz_does_not_require_auth(client):
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"


def test_readyz_reports_both_models_loaded_in_mock_mode(client):
    r = client.get("/readyz")
    assert r.status_code == 200
    body = r.json()
    assert body["plapre"] is True
    assert body["hviske"] is True  # hviske now implemented (FIR-1637).


def test_synthesize_requires_bearer_token(client):
    r = client.post("/plapre/synthesize", json={"text": "hej"})
    assert r.status_code == 401


def test_synthesize_rejects_wrong_token(client):
    r = client.post(
        "/plapre/synthesize",
        json={"text": "hej"},
        headers={"Authorization": "Bearer wrong"},
    )
    assert r.status_code == 401


def test_voices_requires_auth(client):
    r = client.get("/plapre/voices")
    assert r.status_code == 401


def test_voices_lists_five_voices(client, auth_headers):
    r = client.get("/plapre/voices", headers=auth_headers)
    assert r.status_code == 200
    voices = r.json()["voices"]
    assert len(voices) == 5
    assert all("voice_id" in v and "label" in v and "gender" in v for v in voices)


def test_hviske_transcribe_requires_auth(client):
    r = client.post(
        "/hviske/transcribe",
        files={"file": ("clip.webm", b"abcdef", "audio/webm")},
    )
    assert r.status_code == 401


def test_hviske_transcribe_returns_text_in_mock_mode(client, auth_headers):
    # Mock hviske echoes a deterministic marker, so a round-trip is observable
    # without a GPU model.
    r = client.post(
        "/hviske/transcribe",
        headers=auth_headers,
        files={"file": ("clip.webm", b"abcdef", "audio/webm")},
    )
    assert r.status_code == 200
    body = r.json()
    assert "text" in body
    assert body["language"] == "da"


def test_hviske_glossary_is_not_used_during_audio_decoding(client, auth_headers):
    r = client.post(
        "/hviske/transcribe",
        headers=auth_headers,
        files={"file": ("clip.webm", b"abcdef", "audio/webm")},
        data={"glossary": "Helsebixen, made4men"},
    )

    assert r.status_code == 200
    assert "+glossary" not in r.json()["text"]


def test_hviske_transcribe_rejects_empty_upload(client, auth_headers):
    r = client.post(
        "/hviske/transcribe",
        headers=auth_headers,
        files={"file": ("clip.webm", b"", "audio/webm")},
    )
    assert r.status_code == 400
