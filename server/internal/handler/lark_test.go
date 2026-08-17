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
	"github.com/multica-ai/multica/server/internal/util/secretbox"
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

// wireLarkInstallServices attaches a live InstallationService and a
// RegistrationService (pointed at a hermetic fake device-flow server) to the
// shared testHandler, restoring the previous values on cleanup. The fake
// answers the RFC 8628 begin call with a canned device_code so an authorized
// BeginLarkInstall reaches a 200; poll calls get authorization_pending so the
// background poller stays quiet for the life of the test.
func wireLarkInstallServices(t *testing.T) {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	installSvc, err := lark.NewInstallationService(testHandler.Queries, box)
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("action") == "begin" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "dc_test",
				"verification_uri_complete": "https://accounts.feishu.cn/oauth/v1/qrcode?code=abc",
				"verification_uri":          "https://accounts.feishu.cn/oauth/v1/qrcode",
				"user_code":                 "ABCD-EFGH",
				"interval":                  1,
				"expire_in":                 2,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
	}))
	t.Cleanup(fake.Close)

	regSvc, err := lark.NewRegistrationService(
		lark.RegistrationServiceConfig{},
		lark.NewRegistrationClient(lark.RegistrationConfig{Domain: fake.URL}),
		lark.NewHTTPAPIClient(lark.HTTPClientConfig{}),
		testHandler.Queries,
		testPool,
		installSvc,
		lark.NewBindingTokenService(testHandler.Queries, testPool),
	)
	if err != nil {
		t.Fatalf("NewRegistrationService: %v", err)
	}

	prevInstall, prevReg := testHandler.LarkInstallations, testHandler.LarkRegistration
	testHandler.LarkInstallations = installSvc
	testHandler.LarkRegistration = regSvc
	t.Cleanup(func() {
		testHandler.LarkInstallations = prevInstall
		testHandler.LarkRegistration = prevReg
	})
}

func beginLarkInstallAs(userID, agentID string) *httptest.ResponseRecorder {
	req := newRequestAs(userID, http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/lark/install/begin?agent_id="+agentID, nil)
	req = withURLParams(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.BeginLarkInstall(w, req)
	return w
}

// TestBeginLarkInstall_AuthorizesAgentOwnerAndAdmins is the core of MUL-4213:
// the device-flow scan-to-bind is authorized by canManageAgent, so the agent's
// owner (a plain workspace member) and workspace owner/admins may begin an
// install, while a member who is neither is forbidden.
func TestBeginLarkInstall_AuthorizesAgentOwnerAndAdmins(t *testing.T) {
	wireLarkInstallServices(t)
	agentID, ownerID, memberID := privateAgentTestFixture(t)

	cases := []struct {
		name   string
		userID string
		want   int
	}{
		{"workspace owner (admin path)", testUserID, http.StatusOK},
		{"agent owner (plain member)", ownerID, http.StatusOK},
		{"unrelated plain member", memberID, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := beginLarkInstallAs(tc.userID, agentID)
			if w.Code != tc.want {
				t.Fatalf("BeginLarkInstall as %s: want %d, got %d: %s",
					tc.name, tc.want, w.Code, w.Body.String())
			}
			if tc.want == http.StatusOK {
				var resp BeginLarkInstallResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("decode begin response: %v", err)
				}
				if resp.SessionID == "" || resp.QRCodeURL == "" {
					t.Fatalf("expected a session id and QR URL, got %+v", resp)
				}
			}
		})
	}
}

// TestGetLarkInstallStatus_ScopedToInitiatorOrAdmin verifies the status poll is
// readable by the session's initiator (the agent owner who began it) and by a
// workspace owner/admin, but returns 404 (no existence leak) to an unrelated
// member (MUL-4213).
func TestGetLarkInstallStatus_ScopedToInitiatorOrAdmin(t *testing.T) {
	wireLarkInstallServices(t)
	agentID, ownerID, memberID := privateAgentTestFixture(t)

	// The agent owner begins the install, so they are the session initiator.
	begin := beginLarkInstallAs(ownerID, agentID)
	if begin.Code != http.StatusOK {
		t.Fatalf("begin as agent owner: want 200, got %d: %s", begin.Code, begin.Body.String())
	}
	var beginResp BeginLarkInstallResponse
	if err := json.Unmarshal(begin.Body.Bytes(), &beginResp); err != nil {
		t.Fatalf("decode begin response: %v", err)
	}

	status := func(userID string) int {
		req := newRequestAs(userID, http.MethodGet,
			"/api/workspaces/"+testWorkspaceID+"/lark/install/"+beginResp.SessionID+"/status", nil)
		req = withURLParams(req, "id", testWorkspaceID, "sessionId", beginResp.SessionID)
		w := httptest.NewRecorder()
		testHandler.GetLarkInstallStatus(w, req)
		return w.Code
	}

	if code := status(ownerID); code != http.StatusOK {
		t.Fatalf("status as initiator (agent owner): want 200, got %d", code)
	}
	if code := status(testUserID); code != http.StatusOK {
		t.Fatalf("status as workspace owner/admin: want 200, got %d", code)
	}
	if code := status(memberID); code != http.StatusNotFound {
		t.Fatalf("status as unrelated member: want 404, got %d", code)
	}
}

// TestRevokeLarkInstallation_AuthorizesAgentOwnerAndAdmins mirrors the bind
// authorization on the unbind path: the bound agent's owner (a plain member)
// and workspace owner/admins may revoke, an unrelated member may not (MUL-4213).
func TestRevokeLarkInstallation_AuthorizesAgentOwnerAndAdmins(t *testing.T) {
	wireLarkInstallServices(t)
	agentID, ownerID, memberID := privateAgentTestFixture(t)

	// A unique (workspace_id, agent_id, channel_type) constraint means at
	// most one Feishu row per agent, so each subcase starts from a clean
	// active row (delete any prior, then insert).
	seedInstallation := func() string {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'feishu'`,
			testWorkspaceID, agentID); err != nil {
			t.Fatalf("clear prior installation: %v", err)
		}
		var instID string
		if err := testPool.QueryRow(context.Background(), `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', 'cli_revoke_test'), $3, 'active')
RETURNING id
`, testWorkspaceID, agentID, ownerID).Scan(&instID); err != nil {
			t.Fatalf("seed installation: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, instID)
		})
		return instID
	}

	revoke := func(userID, instID string) int {
		req := newRequestAs(userID, http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/lark/installations/"+instID, nil)
		req = withURLParams(req, "id", testWorkspaceID, "installationId", instID)
		w := httptest.NewRecorder()
		testHandler.RevokeLarkInstallation(w, req)
		return w.Code
	}

	// Unrelated member: forbidden.
	if code := revoke(memberID, seedInstallation()); code != http.StatusForbidden {
		t.Fatalf("revoke as unrelated member: want 403, got %d", code)
	}
	// Agent owner: allowed.
	if code := revoke(ownerID, seedInstallation()); code != http.StatusNoContent {
		t.Fatalf("revoke as agent owner: want 204, got %d", code)
	}
	// Workspace owner/admin: allowed.
	if code := revoke(testUserID, seedInstallation()); code != http.StatusNoContent {
		t.Fatalf("revoke as workspace owner: want 204, got %d", code)
	}
}

// TestRevokeLarkInstallation_OrphanCleanableByAdminNotMember pins the
// orphan-cleanup path (Elon review on MUL-4213 / PR #5079): when the bound
// agent has been hard-deleted, the agent-owner authorization has no agent to
// resolve, so revoke must fall back to workspace owner/admin — a workspace
// owner can still disconnect the orphan (the documented cleanup entry point),
// while a plain member cannot.
func TestRevokeLarkInstallation_OrphanCleanableByAdminNotMember(t *testing.T) {
	wireLarkInstallServices(t)
	// Only need a plain member; the installation binds a deleted agent.
	_, _, memberID := privateAgentTestFixture(t)

	// agent_id references no agent row (hard-deleted / never existed). There
	// is no FK, so the row persists as an orphan — exactly the ListByWorkspace
	// case the active-connection query filters out.
	const orphanAgent = "5d0a0000-0000-4000-8000-0000000000aa"
	seedOrphan := func() string {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'feishu'`,
			testWorkspaceID, orphanAgent); err != nil {
			t.Fatalf("clear prior orphan: %v", err)
		}
		var instID string
		if err := testPool.QueryRow(context.Background(), `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', 'cli_orphan'), $3, 'active')
RETURNING id
`, testWorkspaceID, orphanAgent, testUserID).Scan(&instID); err != nil {
			t.Fatalf("seed orphan installation: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, instID)
		})
		return instID
	}

	revoke := func(userID, instID string) int {
		req := newRequestAs(userID, http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/lark/installations/"+instID, nil)
		req = withURLParams(req, "id", testWorkspaceID, "installationId", instID)
		w := httptest.NewRecorder()
		testHandler.RevokeLarkInstallation(w, req)
		return w.Code
	}

	// Plain member: no agent to own, so orphan cleanup is denied.
	if code := revoke(memberID, seedOrphan()); code != http.StatusForbidden {
		t.Fatalf("orphan revoke as plain member: want 403, got %d", code)
	}
	// Workspace owner/admin: can disconnect the orphan.
	if code := revoke(testUserID, seedOrphan()); code != http.StatusNoContent {
		t.Fatalf("orphan revoke as workspace owner: want 204, got %d", code)
	}
}
