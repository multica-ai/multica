package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

// Lark-handler unit tests focus on the no-config short-circuits —
// verifying that a self-host deployment without MULTICA_LARK_SECRET_KEY
// does NOT serve revoke / redeem / install, and that list degrades
// gracefully to an empty response so the Integrations tab still
// renders. Happy-path flows (begin device-flow + poll status; token
// mint + redeem) need a real DB and land alongside the WS hub
// integration tests in a follow-up commit.

func TestRevokeLarkInstallation_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/x/lark/installations/y", nil)
	w := httptest.NewRecorder()
	h.RevokeLarkInstallation(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestRedeemLarkBindingToken_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/lark/binding/redeem", strings.NewReader(`{"token":"x"}`))
	w := httptest.NewRecorder()
	h.RedeemLarkBindingToken(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestBeginLarkInstall_NotConfigured(t *testing.T) {
	// When the device-flow registration service is nil (no at-rest
	// key, or the stub APIClient is the only one wired), the begin
	// endpoint must short-circuit to 503 — silently returning a
	// "configured: false" envelope would hide a real misconfiguration
	// from the operator. The UI hides the bind button in that case
	// so this should not be reached through the normal flow.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/x/lark/install/begin?agent_id=y", nil)
	w := httptest.NewRecorder()
	h.BeginLarkInstall(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetLarkInstallStatus_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/install/sess_y/status", nil)
	w := httptest.NewRecorder()
	h.GetLarkInstallStatus(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestListLarkInstallations_NotConfiguredReturnsEmpty(t *testing.T) {
	// Listing is intentionally a "soft" endpoint: when lark is not
	// configured we return an empty list + configured:false rather
	// than a 503, so the Integrations tab renders normally with a
	// "not connected" empty state instead of an error banner.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
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
		t.Fatalf("configured should be false when LarkInstallations is nil")
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported should be false when LarkInstallations is nil")
	}
	if len(resp.Installations) != 0 {
		t.Fatalf("expected empty installations list, got %d", len(resp.Installations))
	}
}

// TestListLarkInstallations_StubClientReportsInstallNotSupported pins
// the front-half of the "don't expose a doomed install flow"
// guarantee: even when the at-rest key + registration service are set,
// install_supported flips false if the underlying APIClient is the
// stub. The stub cannot complete the post-poll GetBotInfo call that
// finalizes a device-flow install, so the UI must hide install entry
// points until a real client is wired.
func TestListLarkInstallations_StubClientReportsInstallNotSupported(t *testing.T) {
	stubLogger := slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil))
	h := &Handler{
		LarkAPIClient: lark.NewStubAPIClient(stubLogger),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Configured       bool `json:"configured"`
		InstallSupported bool `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported must be false while only stub APIClient is wired")
	}
}

// TestListLarkInstallations_NotConfigured_HardCodedInstallSupportedFalse
// pins the invariant for the early-return branch: when
// LarkInstallations is nil (the deployment has no at-rest encryption
// key wired), the response MUST return both configured:false AND
// install_supported:false regardless of what APIClient is in place.
// A real APIClient on a not-configured deployment must not flip
// install_supported via the APIClient path — that path is not
// consulted in the early-return branch.
func TestListLarkInstallations_NotConfigured_HardCodedInstallSupportedFalse(t *testing.T) {
	stubLogger := slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil))
	h := &Handler{
		LarkInstallations: nil, // triggers the not-configured early return.
		LarkAPIClient:     lark.NewStubAPIClient(stubLogger),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Configured       bool `json:"configured"`
		InstallSupported bool `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured {
		t.Fatalf("configured must be false when LarkInstallations is nil")
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported must be false in the early-return branch even with a non-nil APIClient")
	}
}

// TestListActiveLarkInstallations_SkipsOrphans pins the MUL-3515 hub-boot
// guard: ListActiveChannelInstallations is JOINed to live workspace + agent,
// so an active channel_installation whose workspace or agent has been deleted
// (channel_* has no FK cascade) is never returned — otherwise the Hub would
// keep opening a WebSocket for a bot whose owner is gone. It also stays
// channel_type='feishu'-scoped. Runs against the real test DB.
func TestListActiveLarkInstallations_SkipsOrphans(t *testing.T) {
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "LarkActiveScopeAgent", []byte("[]"))

	const (
		liveApp   = "cli_active_live"
		orphanApp = "cli_active_orphan"
		slackApp  = "cli_active_slack"
		// Deliberately non-existent workspace/agent so the JOIN drops the row.
		orphanWS = "5d0a0000-0000-4000-8000-000000000001"
		orphanAg = "5d0a0000-0000-4000-8000-000000000002"
	)
	clean := func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`,
			[]string{liveApp, orphanApp, slackApp})
	}
	clean()
	t.Cleanup(clean)

	seed := func(ws, ag, channelType, app string) {
		if _, err := testPool.Exec(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, $3, jsonb_build_object('app_id', $4::text), $5, 'active')
`, ws, ag, channelType, app, testUserID); err != nil {
			t.Fatalf("seed %s installation: %v", app, err)
		}
	}
	seed(testWorkspaceID, agentID, "feishu", liveApp) // live workspace + agent -> listed
	seed(orphanWS, orphanAg, "feishu", orphanApp)     // deleted workspace + agent -> dropped
	seed(testWorkspaceID, agentID, "slack", slackApp) // wrong channel_type -> dropped

	active, err := lark.NewChannelStore(testHandler.Queries).ListActiveLarkInstallations(ctx)
	if err != nil {
		t.Fatalf("ListActiveLarkInstallations: %v", err)
	}
	seen := map[string]bool{}
	for _, inst := range active {
		seen[inst.AppID] = true
	}
	if !seen[liveApp] {
		t.Fatal("expected the live-workspace/agent Feishu installation to be listed")
	}
	if seen[orphanApp] {
		t.Fatal("orphaned installation (deleted workspace/agent) must not be listed — the hub would connect a dead bot")
	}
	if seen[slackApp] {
		t.Fatal("non-Feishu installation must not be listed by the Feishu hub")
	}
}
