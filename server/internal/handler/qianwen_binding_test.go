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
		_ = service.Revoke(context.Background(), installed.Installation.ID)
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
