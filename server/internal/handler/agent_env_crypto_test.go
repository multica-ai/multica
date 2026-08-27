package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentEnvCrypto_RoundTrip proves the envelope is opaque at rest but
// round-trips back to the exact plaintext.
func TestAgentEnvCrypto_RoundTrip(t *testing.T) {
	t.Setenv(envEncKeyEnv, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	plain := []byte(`{"API_KEY":"v","OTHER":"y"}`)
	enc, err := encryptCustomEnv(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(enc) == string(plain) {
		t.Fatalf("ciphertext must differ from plaintext")
	}
	if strings.Contains(string(enc), "API_KEY") {
		t.Fatalf("ciphertext must not leak the secret key name: %s", enc)
	}
	dec, err := decryptCustomEnv(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("round trip mismatch: got %s want %s", dec, plain)
	}
}

// TestAgentEnvCrypto_KeyMissingDegradesToPlaintext proves that with
// MULTICA_ENV_ENC_KEY unset the value is stored verbatim (safe degrade —
// the deploy keeps working rather than refusing).
func TestAgentEnvCrypto_KeyMissingDegradesToPlaintext(t *testing.T) {
	t.Setenv(envEncKeyEnv, "")
	plain := []byte(`{"K":"V"}`)
	enc, err := encryptCustomEnv(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(enc) != string(plain) {
		t.Fatalf("with no key, env must be stored plaintext, got %s", enc)
	}
}

// TestAgentEnvCrypto_BackwardCompatNonEnvelope proves a pre-encryption
// plaintext row is returned as-is instead of being mistaken for an envelope.
func TestAgentEnvCrypto_BackwardCompatNonEnvelope(t *testing.T) {
	t.Setenv(envEncKeyEnv, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	plain := []byte(`{"API_KEY":"plain"}`)
	dec, err := decryptCustomEnv(plain)
	if err != nil {
		t.Fatalf("decrypt plaintext: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("plaintext should pass through unchanged, got %s", dec)
	}
}

// TestAgentEnvCrypto_InvalidKeyRefuses proves a malformed key is rejected
// rather than silently producing a weak/garbage ciphertext.
func TestAgentEnvCrypto_InvalidKeyRefuses(t *testing.T) {
	t.Setenv(envEncKeyEnv, "tooshort")
	if _, err := encryptCustomEnv([]byte("x")); err == nil {
		t.Fatalf("expected error for invalid key length")
	}
}

// TestAgentEnv_EncryptionAtRest is the end-to-end proof for GAP-10 Phase 1:
//  1. a pre-existing plaintext row still reads back as plaintext (backward
//     compat with the envelope-detection read path);
//  2. after an update with MULTICA_ENV_ENC_KEY set, the value persisted in
//     the DB is a v1 envelope that does NOT contain the secret; and
//  3. GetAgentEnv returns the original plaintext to an authorized caller.
func TestAgentEnv_EncryptionAtRest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Enable at-rest encryption for the duration of this test only.
	t.Setenv(envEncKeyEnv, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	ctx := context.Background()

	agentID, ownerUserID := agentEnvOwnerFixture(t, "env-enc-agent", "env-enc-owner@multica.test")
	// The fixture seeded a plaintext row (the backward-compat path).

	// 1. Pre-existing plaintext row still reads back correctly.
	req := withURLParam(newRequestAs(ownerUserID, http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv (pre-existing plaintext): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode reveal response: %v", err)
	}
	if resp.CustomEnv["API_KEY"] != "secret-value" {
		t.Fatalf("backward-compat read: expected secret-value, got %v", resp.CustomEnv)
	}

	// 2. Update with encryption on; the persisted value must be ciphertext.
	body := map[string]any{"custom_env": map[string]string{"API_KEY": "super-secret-at-rest"}}
	req = withURLParam(newRequestAs(ownerUserID, http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
	w = httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back custom_env: %v", err)
	}
	if strings.Contains(stored, "super-secret-at-rest") || strings.Contains(stored, "API_KEY") {
		t.Fatalf("custom_env at rest must be ciphertext and must not contain the secret; got: %s", stored)
	}
	var env envEnvelope
	if err := json.Unmarshal([]byte(stored), &env); err != nil || env.Enc != envSchemeV1 {
		t.Fatalf("stored custom_env must be a v1 envelope, got: %s", stored)
	}

	// 3. GetAgentEnv returns the plaintext to an authorized caller.
	req = withURLParam(newRequestAs(ownerUserID, http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	w = httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv (after update): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode reveal response: %v", err)
	}
	if resp.CustomEnv["API_KEY"] != "super-secret-at-rest" {
		t.Fatalf("expected decrypted plaintext, got %v", resp.CustomEnv)
	}
}
