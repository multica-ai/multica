def test_healthz_does_not_require_auth(client):
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"


def test_readyz_reports_plapre_loaded_in_mock_mode(client):
    r = client.get("/readyz")
    assert r.status_code == 200
    body = r.json()
    assert body["plapre"] is True
    assert body["hviske"] is False  # Stubbed; JEH-729 slice 4.


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


def test_hviske_transcribe_returns_503_until_jeh729_lands(client, auth_headers):
    # Reachable + auth-protected, just not implemented yet.
    r = client.post("/hviske/transcribe", headers=auth_headers)
    assert r.status_code == 503
