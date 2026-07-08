package handler

import (
	"encoding/json"
	"testing"
)

func TestMaskGatewayTokenReplacesNonEmpty(t *testing.T) {
	t.Parallel()

	rc := map[string]any{
		"mode": "gateway",
		"gateway": map[string]any{
			"host":  "gw.internal",
			"port":  float64(18789), // json.Unmarshal yields float64 for numbers
			"token": "real-secret",
			"tls":   true,
		},
	}
	maskGatewayToken(rc)
	gw := rc["gateway"].(map[string]any)
	if gw["token"] != runtimeConfigGatewayTokenMask {
		t.Errorf("token: got %v, want %q", gw["token"], runtimeConfigGatewayTokenMask)
	}
	if gw["host"] != "gw.internal" {
		t.Errorf("host must not be touched, got %v", gw["host"])
	}
}

func TestMaskGatewayTokenSkipsEmptyToken(t *testing.T) {
	t.Parallel()

	// host+port-only configs (token still inherited from the user's local
	// openclaw.json) must not surface a misleading "***" placeholder.
	rc := map[string]any{
		"gateway": map[string]any{
			"host": "gw.internal",
			"port": float64(18789),
		},
	}
	maskGatewayToken(rc)
	gw := rc["gateway"].(map[string]any)
	if _, present := gw["token"]; present {
		t.Errorf("empty token must not gain a mask, got %v", gw["token"])
	}
}

func TestMaskGatewayTokenNoOpOnNonOpenclawShape(t *testing.T) {
	t.Parallel()

	// rc with no `gateway` key (e.g. other providers' runtime_config) must
	// pass through untouched.
	rc := map[string]any{"some_other_key": "value"}
	maskGatewayToken(rc)
	if _, present := rc["gateway"]; present {
		t.Errorf("must not synthesise gateway key, got %v", rc)
	}
}

func TestPreserveMaskedGatewayTokenRestoresFromPersisted(t *testing.T) {
	t.Parallel()

	persisted := []byte(`{"mode":"gateway","gateway":{"token":"real-secret","host":"gw.internal"}}`)
	incoming := map[string]any{
		"mode": "gateway",
		"gateway": map[string]any{
			"host":  "gw.internal",
			"port":  float64(18789),
			"token": runtimeConfigGatewayTokenMask,
		},
	}
	preserveMaskedGatewayToken(incoming, persisted)
	gw := incoming["gateway"].(map[string]any)
	if gw["token"] != "real-secret" {
		t.Errorf("token should be restored from persisted row, got %v", gw["token"])
	}
}

func TestPreserveMaskedGatewayTokenPassesThroughRealValue(t *testing.T) {
	t.Parallel()

	// A genuine new token in the PATCH body must overwrite the persisted one.
	persisted := []byte(`{"gateway":{"token":"old-secret"}}`)
	incoming := map[string]any{
		"gateway": map[string]any{"token": "rotated-secret"},
	}
	preserveMaskedGatewayToken(incoming, persisted)
	gw := incoming["gateway"].(map[string]any)
	if gw["token"] != "rotated-secret" {
		t.Errorf("real PATCH token must win, got %v", gw["token"])
	}
}

func TestPreserveMaskedGatewayTokenDropsMaskWhenNoPersistedToken(t *testing.T) {
	t.Parallel()

	// A first-time gateway config that only contained host/port has no
	// stored token. If a later PATCH sends the mask back (e.g. a UI that
	// always includes the field), we must drop the placeholder rather than
	// landing the literal "***" string in the database as a fake bearer.
	persisted := []byte(`{"gateway":{"host":"gw.internal"}}`)
	incoming := map[string]any{
		"gateway": map[string]any{"token": runtimeConfigGatewayTokenMask},
	}
	preserveMaskedGatewayToken(incoming, persisted)
	gw := incoming["gateway"].(map[string]any)
	if _, present := gw["token"]; present {
		t.Errorf("token must be dropped, got %v", gw["token"])
	}
}

// Round-trip: marshal a runtime_config, mask it, ensure it stays a valid
// shape that can survive json.Marshal again (no NaNs, no funny types).
func TestMaskGatewayTokenRoundTripsAsJSON(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"gateway":{"token":"plaintext","host":"gw"}}`)
	var rc any
	if err := json.Unmarshal(raw, &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	maskGatewayToken(rc)
	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal after mask: %v", err)
	}
	if string(out) == string(raw) {
		t.Errorf("mask should change the bytes, got %q", out)
	}
}
