package tagaccess

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

type assertionContractFixture struct {
	TestKeyBase64 string `json:"testKeyBase64"`
	Vectors       []struct {
		Name          string        `json:"name"`
		RawBodyBase64 string        `json:"rawBodyBase64"`
		Payload       HTTPAssertion `json:"payload"`
		Canonical     string        `json:"canonical"`
		Signature     string        `json:"signature"`
	} `json:"vectors"`
}

func TestHTTPAssertionVerifierConsumesVIBESCrossLanguageFixture(t *testing.T) {
	fixtureBytes, err := os.ReadFile("testdata/tag-gateway-assertion-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture assertionContractFixture
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	vector := fixture.Vectors[0]
	body, err := base64.StdEncoding.DecodeString(vector.RawBodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewHTTPAssertionVerifier(
		map[string][]byte{vector.Payload.KeyID: key},
		assertionClock{now: time.UnixMilli(vector.Payload.IssuedAt + 1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		vector.Payload.Method,
		vector.Payload.Path+"?"+vector.Payload.Query,
		bytes.NewReader(body),
	)
	payload, err := json.Marshal(vector.Payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
	request.Header.Set(HTTPAssertionSignatureHeader, vector.Signature)
	request.Header.Set(HTTPAssertionKeyIDHeader, vector.Payload.KeyID)

	if _, err := verifier.VerifyRequest(request); err != nil {
		t.Fatalf("VIBES fixture rejected: %v", err)
	}
}

func TestWebSocketAssertionVerifierConsumesVIBESCrossLanguageFixture(t *testing.T) {
	fixtureBytes, err := os.ReadFile("testdata/tag-gateway-assertion-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture assertionContractFixture
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) < 2 || fixture.Vectors[1].Name != "websocket-upgrade" {
		t.Fatalf("missing exact WS fixture vector: %#v", fixture.Vectors)
	}
	vector := fixture.Vectors[1]
	key, err := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewWebSocketAssertionVerifier(
		map[string][]byte{vector.Payload.KeyID: key},
		assertionClock{now: time.UnixMilli(vector.Payload.IssuedAt + 1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(vector.Payload.Method, vector.Payload.Path+"?"+vector.Payload.Query, nil)
	payload, err := json.Marshal(vector.Payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
	request.Header.Set(HTTPAssertionSignatureHeader, vector.Signature)
	request.Header.Set(HTTPAssertionKeyIDHeader, vector.Payload.KeyID)

	verified, err := verifier.VerifyRequest(request)
	if err != nil {
		t.Fatalf("VIBES WS fixture rejected: %v", err)
	}
	if verified.SessionWorkspaceGeneration != vector.Payload.SessionWorkspaceGeneration {
		t.Fatalf("verified generation = %d, want %d", verified.SessionWorkspaceGeneration, vector.Payload.SessionWorkspaceGeneration)
	}
}
