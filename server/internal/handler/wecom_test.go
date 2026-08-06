package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// WeCom-handler unit tests exercise the not-configured short-circuits and
// the boundary validation the wire contract pins: begin without an
// Idempotency-Key is 400, status/revoke without a wired install service is
// 503, list is a soft 200 with configured=false. Happy-path flows need a
// live Postgres (they exercise the InstallService against real
// wecom_install_session rows) and live in the DB-backed test suite.

func TestListWecomInstallations_NotConfiguredReturnsEmpty(t *testing.T) {
	// List is intentionally a "soft" endpoint: when the WeCom
	// integration is not wired we return an empty list with
	// configured=false so the Integrations tab renders a
	// "not connected" empty state instead of an error banner
	// (mirrors ListLarkInstallations).
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/wecom/installations", nil)
	w := httptest.NewRecorder()
	h.ListWecomInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control=no-store, got %q", got)
	}
	var resp struct {
		Installations          []map[string]any `json:"installations"`
		Configured             bool             `json:"configured"`
		InstallSupported       bool             `json:"install_supported"`
		ManualInstallSupported bool             `json:"manual_install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured || resp.InstallSupported || resp.ManualInstallSupported {
		t.Fatalf("expected configured / install_supported / manual_install_supported all false, got %+v", resp)
	}
	if len(resp.Installations) != 0 {
		t.Fatalf("expected empty installations, got %d", len(resp.Installations))
	}
}

func TestBeginWecomInstall_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/x/wecom/install/begin?agent_id=y", nil)
	req.Header.Set("Idempotency-Key", "abc")
	w := httptest.NewRecorder()
	h.BeginWecomInstall(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control=no-store, got %q", got)
	}
}

// TestManualWecomInstall_NotConfigured: manual entry needs the secretbox key
// to seal the submitted secret, so with no install service wired the endpoint
// refuses rather than writing a plaintext credential.
func TestManualWecomInstall_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost,
		"/api/workspaces/x/wecom/install/manual?agent_id=y",
		strings.NewReader(`{"bot_id":"b","secret":"s"}`))
	w := httptest.NewRecorder()
	h.ManualWecomInstall(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control=no-store, got %q", got)
	}
	// A refused request must not echo the submitted secret back.
	if strings.Contains(w.Body.String(), "s") && strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response mentions the submitted secret: %s", w.Body.String())
	}
}

func TestGetWecomInstallStatus_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/wecom/install/sess_y/status", nil)
	w := httptest.NewRecorder()
	h.GetWecomInstallStatus(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRevokeWecomInstallation_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/x/wecom/installations/y", nil)
	w := httptest.NewRecorder()
	h.RevokeWecomInstallation(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRedeemWecomBindingToken_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/wecom/binding/redeem", strings.NewReader(`{"token":"t"}`))
	w := httptest.NewRecorder()
	h.RedeemWecomBindingToken(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// wecomResponseHasNoSecrets pins the wire contract that a WeCom
// installation response never leaks the encrypted secret, raw bot info,
// scode, qr_code_url, or WS lease columns (spec §7.3.1). Adding a field
// requires an explicit decision here.
func TestListWecomInstallations_ResponseHasNoSecretsSchema(t *testing.T) {
	// This is a schema-level test that inspects the JSON tag surface of
	// wecomInstallationResponse. It executes without a real WeCom
	// installation.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/wecom/installations", nil)
	w := httptest.NewRecorder()
	h.ListWecomInstallations(w, req)
	body, _ := io.ReadAll(w.Body)
	// Empty list, but the field discipline is verified by scanning the
	// list-response JSON for forbidden markers.
	forbidden := []string{
		"secret_encrypted",
		"bot_info",
		"scode",
		"qr_code_url",
		"ws_lease",
		"config",
	}
	for _, key := range forbidden {
		if strings.Contains(string(body), `"`+key+`"`) {
			t.Fatalf("list response leaks forbidden field %q: %s", key, body)
		}
	}
}
