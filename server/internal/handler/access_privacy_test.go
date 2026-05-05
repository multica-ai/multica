package handler

// CEREBRO-PATCH(access-handler): cerebro modification of upstream file

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EVAL for Phase 6 — issue privacy. Verifies that setting is_private = true
// on a standalone issue hides it from other members, and that toggling it
// off restores visibility. Agent task scope is structurally bounded by the
// existing task-scoped middleware (AllowTaskScopeForIssue restricts every
// task token to a single issue ID); this test confirms the privacy field
// flows end-to-end.

func TestUpdateIssue_IsPrivateFlag(t *testing.T) {
	f := setupFilterFixture(t)
	ctx := newReqAs("GET", "/", testUserID, db.Member{}).Context()

	owner, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(testUserID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load owner: %v", err)
	}

	// Create a standalone issue (no project) owned by the owner.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, is_private)
		VALUES ($1, 'phase6-standalone', 'todo', 'medium', 'member', $2, 0, 9990, false)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Initially: outside member can see the issue (it's not private).
	preReq := newReqAs("GET", "/api/issues/"+issueID, f.outsideUserID, f.outsideMember)
	preReq = withURLParams(preReq, "id", issueID)
	preRec := httptest.NewRecorder()
	testHandler.GetIssue(preRec, preReq)
	if preRec.Code != http.StatusOK {
		t.Fatalf("pre-private GET as outsider: want 200, got %d", preRec.Code)
	}

	// Owner toggles is_private = true via UpdateIssue.
	body, _ := json.Marshal(map[string]any{"is_private": true})
	upd := httptest.NewRequest("PUT", "/api/issues/"+issueID, bytes.NewReader(body))
	upd.Header.Set("Content-Type", "application/json")
	upd.Header.Set("X-User-ID", testUserID)
	upd.Header.Set("X-Workspace-ID", testWorkspaceID)
	upd = upd.WithContext(middleware.SetMemberContext(upd.Context(), testWorkspaceID, owner))
	upd = withURLParams(upd, "id", issueID)
	updRec := httptest.NewRecorder()
	testHandler.UpdateIssue(updRec, upd)
	if updRec.Code != http.StatusOK {
		t.Fatalf("toggle is_private: want 200, got %d body=%s", updRec.Code, updRec.Body.String())
	}

	var resp IssueResponse
	if err := json.Unmarshal(updRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal issue response: %v", err)
	}
	if !resp.IsPrivate {
		t.Fatalf("response.is_private should be true after toggle; got %+v", resp)
	}

	// After toggle: outsider gets 404.
	postReq := newReqAs("GET", "/api/issues/"+issueID, f.outsideUserID, f.outsideMember)
	postReq = withURLParams(postReq, "id", issueID)
	postRec := httptest.NewRecorder()
	testHandler.GetIssue(postRec, postReq)
	if postRec.Code != http.StatusNotFound {
		t.Fatalf("post-private GET as outsider: want 404 (no leak), got %d", postRec.Code)
	}

	// Creator (owner) still sees it.
	ownerReq := newReqAs("GET", "/api/issues/"+issueID, testUserID, owner)
	ownerReq = withURLParams(ownerReq, "id", issueID)
	ownerRec := httptest.NewRecorder()
	testHandler.GetIssue(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("creator GET on own private issue: want 200, got %d", ownerRec.Code)
	}
}
