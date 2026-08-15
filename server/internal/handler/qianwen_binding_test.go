package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// UnbindCurrentUser is part of the management contract exercised below. Keep
// the shared fake forward-compatible with that contract so the existing
// Qianwen handler tests continue to compile once the production interface grows
// the method. Tests that need observable unbind state use the real Service.
func (f *fakeQianwenService) UnbindCurrentUser(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error {
	return nil
}

type fakeQianwenBindingManagementService struct {
	*fakeQianwenService
	unbindFn func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error
}

func (f *fakeQianwenBindingManagementService) UnbindCurrentUser(
	ctx context.Context,
	workspaceID pgtype.UUID,
	installationID pgtype.UUID,
	userID pgtype.UUID,
) error {
	if f.unbindFn == nil {
		return nil
	}
	return f.unbindFn(ctx, workspaceID, installationID, userID)
}

func newQianwenBindingHandlerService(t *testing.T) *qianwen.Service {
	t.Helper()

	sessions := engine.NewChatSession(testHandler.Queries, testPool, qianwen.TypeQianwen, engine.SessionTitles{
		Direct:   "Qianwen glasses request",
		Fallback: "Qianwen glasses request",
	})
	service, err := qianwen.NewService(
		testHandler.Queries,
		sessions,
		testHandler.TaskService,
		testPool,
		[]byte("qianwen-handler-binding-test-deployment-secret"),
	)
	if err != nil {
		t.Fatalf("construct Qianwen binding-management service: %v", err)
	}
	return service
}

func installQianwenBindingHandlerFixture(
	t *testing.T,
	service *qianwen.Service,
	agentID string,
) qianwen.InstallationResult {
	t.Helper()

	installed, err := service.InstallPersonal(
		context.Background(),
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(agentID),
		util.MustParseUUID(testUserID),
	)
	if err != nil {
		t.Fatalf("install Qianwen binding-management fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Revoke(context.Background(), util.MustParseUUID(testWorkspaceID), installed.Installation.ID)
		_, _ = testPool.Exec(
			context.Background(),
			`DELETE FROM channel_installation WHERE id = $1`,
			installed.Installation.ID,
		)
	})
	return installed
}

func TestListQianwenInstallationsReturnsCurrentUserBindingWithoutOpaqueIdentity(t *testing.T) {
	service := newQianwenBindingHandlerService(t)
	boundAgentID := createHandlerTestAgent(t, "Qianwen Bound Viewer", []byte(`{}`))
	unboundAgentID := createHandlerTestAgent(t, "Qianwen Unbound Viewer", []byte(`{}`))
	bound := installQianwenBindingHandlerFixture(t, service, boundAgentID)
	unbound := installQianwenBindingHandlerFixture(t, service, unboundAgentID)

	const (
		opaqueOpenUserID = "opaque-qianwen-open-user-must-not-leak"
		opaqueOpenUUID   = "opaque-qianwen-open-uuid-must-not-leak"
	)
	if _, err := testHandler.Queries.CreateChannelUserBinding(
		context.Background(),
		db.CreateChannelUserBindingParams{
			WorkspaceID:    util.MustParseUUID(testWorkspaceID),
			MulticaUserID:  util.MustParseUUID(testUserID),
			InstallationID: bound.Installation.ID,
			ChannelType:    string(qianwen.TypeQianwen),
			ChannelUserID:  opaqueOpenUserID,
			Config:         []byte(`{"open_uuid":"` + opaqueOpenUUID + `","identity_scope":"skill"}`),
		},
	); err != nil {
		t.Fatalf("seed current-user Qianwen binding: %v", err)
	}

	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		listFn: func(_ context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
			if workspaceID != util.MustParseUUID(testWorkspaceID) {
				t.Fatalf("list workspace = %s, want %s", util.UUIDToString(workspaceID), testWorkspaceID)
			}
			return []db.ChannelInstallation{bound.Installation, unbound.Installation}, nil
		},
	}
	req := qianwenRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations",
		"",
		"id", testWorkspaceID,
	)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()

	h.ListQianwenInstallations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if strings.Contains(w.Body.String(), opaqueOpenUserID) || strings.Contains(w.Body.String(), opaqueOpenUUID) {
		t.Fatalf("list response leaked opaque Qianwen identity: %s", w.Body.String())
	}

	var response struct {
		Installations []map[string]json.RawMessage `json:"installations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode installation list: %v", err)
	}
	if len(response.Installations) != 2 {
		t.Fatalf("installation count = %d, want 2; body=%s", len(response.Installations), w.Body.String())
	}

	want := map[string]bool{
		util.UUIDToString(bound.Installation.ID):   true,
		util.UUIDToString(unbound.Installation.ID): false,
	}
	for _, row := range response.Installations {
		var id string
		if err := json.Unmarshal(row["id"], &id); err != nil {
			t.Fatalf("decode installation id: %v; row=%s", err, row["id"])
		}
		wantBound, ok := want[id]
		if !ok {
			t.Fatalf("unexpected installation id %q", id)
		}
		rawBound, present := row["current_user_bound"]
		if !present {
			t.Fatalf("installation %s omitted caller-relative current_user_bound", id)
		}
		var gotBound bool
		if err := json.Unmarshal(rawBound, &gotBound); err != nil {
			t.Fatalf("decode current_user_bound for %s: %v", id, err)
		}
		if gotBound != wantBound {
			t.Fatalf("installation %s current_user_bound = %v, want %v", id, gotBound, wantBound)
		}
	}
}

func TestListQianwenInstallationsFiltersAgentsByCallerViewPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	viewerID := createPermissionTestMember(t, "qianwen-installation-viewer@multica.test")
	ownedAgentID := createHandlerTestAgent(t, "Qianwen Viewer Owned Private", []byte(`{}`))
	sharedAgentID := createHandlerTestAgent(t, "Qianwen Viewer Shared Public", []byte(`{}`))
	hiddenAgentID := createHandlerTestAgent(t, "Qianwen Viewer Hidden Private", []byte(`{}`))

	ctx := context.Background()
	for _, agentID := range []string{ownedAgentID, sharedAgentID, hiddenAgentID} {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID); err != nil {
			t.Fatalf("clear invocation targets for agent %s: %v", agentID, err)
		}
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET owner_id = $2, permission_mode = 'private', visibility = 'private'
		WHERE id = $1
	`, ownedAgentID, viewerID); err != nil {
		t.Fatalf("make viewer-owned agent private: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET permission_mode = 'private', visibility = 'private'
		WHERE id = $1
	`, hiddenAgentID); err != nil {
		t.Fatalf("make hidden agent private: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'member', $2, $3)
	`, sharedAgentID, viewerID, testUserID); err != nil {
		t.Fatalf("share public agent with viewer: %v", err)
	}

	rows := []db.ChannelInstallation{
		{
			ID:          util.MustParseUUID("f0a8479c-d164-4db7-a42a-989127a1f010"),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
			AgentID:     util.MustParseUUID(ownedAgentID),
			ChannelType: string(qianwen.TypeQianwen),
			Config:      []byte(`{"app_id":"qwc_viewer_owned","mode":"personal_polling"}`),
			Status:      "active",
		},
		{
			ID:          util.MustParseUUID("f0a8479c-d164-4db7-a42a-989127a1f011"),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
			AgentID:     util.MustParseUUID(sharedAgentID),
			ChannelType: string(qianwen.TypeQianwen),
			Config:      []byte(`{"app_id":"qwc_shared_with_viewer","mode":"personal_polling"}`),
			Status:      "active",
		},
		{
			ID:          util.MustParseUUID("f0a8479c-d164-4db7-a42a-989127a1f012"),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
			AgentID:     util.MustParseUUID(hiddenAgentID),
			ChannelType: string(qianwen.TypeQianwen),
			Config:      []byte(`{"app_id":"qwc_hidden_private_must_not_leak","mode":"personal_polling"}`),
			Status:      "active",
		},
	}

	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		listFn: func(_ context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
			if workspaceID != util.MustParseUUID(testWorkspaceID) {
				t.Fatalf("list workspace = %s, want %s", util.UUIDToString(workspaceID), testWorkspaceID)
			}
			return rows, nil
		},
	}
	req := qianwenRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations",
		"",
		"id", testWorkspaceID,
	)
	req.Header.Set("X-User-ID", viewerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()

	h.ListQianwenInstallations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "qwc_hidden_private_must_not_leak") ||
		strings.Contains(w.Body.String(), hiddenAgentID) ||
		strings.Contains(w.Body.String(), util.UUIDToString(rows[2].ID)) {
		t.Fatalf("list response leaked a hidden private installation: %s", w.Body.String())
	}

	var response struct {
		Installations []QianwenInstallationResponse `json:"installations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode installation list: %v", err)
	}
	if len(response.Installations) != 2 {
		t.Fatalf("installation count = %d, want viewer-owned and explicitly shared rows; body=%s", len(response.Installations), w.Body.String())
	}
	wantConnectionIDs := map[string]bool{
		"qwc_viewer_owned":       false,
		"qwc_shared_with_viewer": false,
	}
	for _, installation := range response.Installations {
		if _, ok := wantConnectionIDs[installation.ConnectionID]; !ok {
			t.Fatalf("unexpected visible connection id %q; body=%s", installation.ConnectionID, w.Body.String())
		}
		wantConnectionIDs[installation.ConnectionID] = true
	}
	for connectionID, seen := range wantConnectionIDs {
		if !seen {
			t.Fatalf("visible connection id %q missing; body=%s", connectionID, w.Body.String())
		}
	}
}

func TestListQianwenInstallationsKeepsOnlyMinimalBoundRowWithoutAgentView(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	viewerID := createPermissionTestMember(t, "qianwen-hidden-bound-viewer@multica.test")
	boundHiddenAgentID := createHandlerTestAgent(t, "Qianwen Hidden Bound Agent", []byte(`{}`))
	unboundHiddenAgentID := createHandlerTestAgent(t, "Qianwen Hidden Unbound Agent", []byte(`{}`))
	service := newQianwenBindingHandlerService(t)
	boundHidden := installQianwenBindingHandlerFixture(t, service, boundHiddenAgentID)
	unboundHidden := installQianwenBindingHandlerFixture(t, service, unboundHiddenAgentID)

	ctx := context.Background()
	for _, agentID := range []string{boundHiddenAgentID, unboundHiddenAgentID} {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID); err != nil {
			t.Fatalf("clear invocation targets for hidden agent %s: %v", agentID, err)
		}
		if _, err := testPool.Exec(ctx, `
			UPDATE agent SET permission_mode = 'private', visibility = 'private'
			WHERE id = $1
		`, agentID); err != nil {
			t.Fatalf("make agent %s private: %v", agentID, err)
		}
	}

	rows := []db.ChannelInstallation{boundHidden.Installation, unboundHidden.Installation}

	const (
		opaqueOpenUserID = "opaque-hidden-bound-user-must-not-leak"
		opaqueOpenUUID   = "opaque-hidden-bound-uuid-must-not-leak"
	)
	if _, err := testHandler.Queries.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    util.MustParseUUID(testWorkspaceID),
		MulticaUserID:  util.MustParseUUID(viewerID),
		InstallationID: boundHidden.Installation.ID,
		ChannelType:    string(qianwen.TypeQianwen),
		ChannelUserID:  opaqueOpenUserID,
		Config:         []byte(`{"open_uuid":"` + opaqueOpenUUID + `","identity_scope":"skill"}`),
	}); err != nil {
		t.Fatalf("seed hidden current-user Qianwen binding: %v", err)
	}

	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		listFn: func(_ context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
			if workspaceID != util.MustParseUUID(testWorkspaceID) {
				t.Fatalf("list workspace = %s, want %s", util.UUIDToString(workspaceID), testWorkspaceID)
			}
			return rows, nil
		},
	}
	req := qianwenRequest(
		http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations",
		"",
		"id", testWorkspaceID,
	)
	req.Header.Set("X-User-ID", viewerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()

	h.ListQianwenInstallations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	for _, secret := range []string{
		boundHiddenAgentID,
		unboundHiddenAgentID,
		qianwen.DecodePublicConfig(boundHidden.Installation.Config).ConnectionID,
		qianwen.DecodePublicConfig(unboundHidden.Installation.Config).ConnectionID,
		opaqueOpenUserID,
		opaqueOpenUUID,
		util.UUIDToString(unboundHidden.Installation.ID),
	} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("list response leaked hidden installation data %q: %s", secret, w.Body.String())
		}
	}

	var response struct {
		Installations []map[string]json.RawMessage `json:"installations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode installation list: %v", err)
	}
	if len(response.Installations) != 1 {
		t.Fatalf("installation count = %d, want one minimal current-user-bound row; body=%s", len(response.Installations), w.Body.String())
	}
	row := response.Installations[0]
	for _, forbiddenField := range []string{"agent_id", "connection_id", "mode"} {
		if _, present := row[forbiddenField]; present {
			t.Fatalf("minimal current-user-bound row exposed %s: %s", forbiddenField, w.Body.String())
		}
	}
	var id, status string
	var currentUserBound bool
	if err := json.Unmarshal(row["id"], &id); err != nil {
		t.Fatalf("decode minimal installation id: %v; row=%v", err, row)
	}
	if err := json.Unmarshal(row["status"], &status); err != nil {
		t.Fatalf("decode minimal installation status: %v; row=%v", err, row)
	}
	if err := json.Unmarshal(row["current_user_bound"], &currentUserBound); err != nil {
		t.Fatalf("decode minimal installation binding state: %v; row=%v", err, row)
	}
	if id != util.UUIDToString(boundHidden.Installation.ID) || status != "active" || !currentUserBound {
		t.Fatalf("minimal current-user-bound row = (id=%q, status=%q, bound=%v), want (%q, active, true)",
			id, status, currentUserBound, util.UUIDToString(boundHidden.Installation.ID))
	}
}

func TestUnbindQianwenCurrentUserHTTPIsIdempotent(t *testing.T) {
	installationID := util.MustParseUUID("8c3ec7c1-bab0-411a-a95b-4af158ab87f4")
	type unbindCall struct {
		workspaceID    pgtype.UUID
		installationID pgtype.UUID
		userID         pgtype.UUID
	}
	var calls []unbindCall
	h := *testHandler
	h.Qianwen = &fakeQianwenBindingManagementService{
		fakeQianwenService: &fakeQianwenService{},
		unbindFn: func(_ context.Context, workspaceID, gotInstallationID, userID pgtype.UUID) error {
			calls = append(calls, unbindCall{
				workspaceID:    workspaceID,
				installationID: gotInstallationID,
				userID:         userID,
			})
			return nil
		},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		req := qianwenRequest(
			http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/qianwen/installations/"+util.UUIDToString(installationID)+"/bindings/me",
			"",
			"id", testWorkspaceID,
			"installationId", util.UUIDToString(installationID),
		)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		w := httptest.NewRecorder()

		h.UnbindQianwenCurrentUser(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("unbind attempt %d status = %d, want %d; body=%s", attempt, w.Code, http.StatusNoContent, w.Body.String())
		}
		if w.Body.Len() != 0 {
			t.Fatalf("unbind attempt %d returned a body for 204: %q", attempt, w.Body.String())
		}
	}
	if len(calls) != 2 {
		t.Fatalf("UnbindCurrentUser() calls = %d, want 2 idempotent calls", len(calls))
	}
	wantWorkspaceID := util.MustParseUUID(testWorkspaceID)
	wantUserID := util.MustParseUUID(testUserID)
	for i, call := range calls {
		if call.workspaceID != wantWorkspaceID || call.installationID != installationID || call.userID != wantUserID {
			t.Fatalf("UnbindCurrentUser() call %d = (%s, %s, %s), want authenticated tuple (%s, %s, %s)",
				i+1,
				util.UUIDToString(call.workspaceID),
				util.UUIDToString(call.installationID),
				util.UUIDToString(call.userID),
				testWorkspaceID,
				util.UUIDToString(installationID),
				testUserID,
			)
		}
	}
}
