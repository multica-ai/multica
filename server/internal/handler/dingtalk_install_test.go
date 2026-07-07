package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// DingTalk-install handler unit tests focus on the no-config
// short-circuits — verifying that a deployment without
// MULTICA_DINGTALK_SECRET_KEY does NOT serve revoke / install, and that
// list degrades gracefully to an empty response so the Integrations tab
// still renders. Happy-path flows (begin device-flow + poll status)
// need a real DB and are covered by the dingtalk package's service
// tests plus the migration-suite integration tests.

func TestRevokeDingTalkInstallation_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/x/dingtalk/installations/y", nil)
	w := httptest.NewRecorder()
	h.RevokeDingTalkInstallation(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestBeginDingTalkInstall_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/x/dingtalk/install/begin?agent_id=y", nil)
	w := httptest.NewRecorder()
	h.BeginDingTalkInstall(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetDingTalkInstallStatus_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/dingtalk/install/sess_y/status", nil)
	w := httptest.NewRecorder()
	h.GetDingTalkInstallStatus(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestListDingTalkInstallations_NotConfiguredReturnsEmpty(t *testing.T) {
	// Listing is intentionally a "soft" endpoint: when dingtalk is not
	// configured we return an empty list + configured:false rather than
	// a 503, so the Integrations tab renders normally with a "not
	// connected" empty state instead of an error banner.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/dingtalk/installations", nil)
	w := httptest.NewRecorder()
	h.ListDingTalkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Installations    []any `json:"installations"`
		Configured       bool  `json:"configured"`
		InstallSupported bool  `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured {
		t.Fatalf("configured should be false when DingTalkInstallations is nil")
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported should be false when DingTalkInstallations is nil")
	}
	if len(resp.Installations) != 0 {
		t.Fatalf("expected empty installations list, got %d", len(resp.Installations))
	}
}
