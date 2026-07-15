package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

type commentSessionModeCall struct {
	issueID, rootCommentID, mode string
}

type recordingCommentSessionMode struct{ calls []commentSessionModeCall }

func (r *recordingCommentSessionMode) RecordCommentSessionMode(_ context.Context, _ pgx.Tx, issueID, rootCommentID, mode string) error {
	r.calls = append(r.calls, commentSessionModeCall{issueID: issueID, rootCommentID: rootCommentID, mode: mode})
	return nil
}

func TestNormalizeNewThreadSessionModeRejectsLegacyAutoSpellings(t *testing.T) {
	for _, raw := range []string{"auto", " AUTO ", "default", " Default "} {
		if mode, err := normalizeNewThreadSessionMode(&raw, false); err == nil {
			t.Fatalf("normalizeNewThreadSessionMode(%q) = %q, nil; want error", raw, mode)
		}
	}
}

func TestCreateCommentRecordsSelectedSessionModeBeforeTriggering(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}
	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'mode-selected thread') RETURNING id`,
		testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	recorder := &recordingCommentSessionMode{}
	previous := testHandler.CommentSessionMode
	testHandler.CommentSessionMode = recorder
	t.Cleanup(func() { testHandler.CommentSessionMode = previous })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Plan this", "session_mode": "plan",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response CommentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("recorder calls = %d, want 1", len(recorder.calls))
	}
	want := commentSessionModeCall{issueID: issueID, rootCommentID: response.ID, mode: "plan"}
	if recorder.calls[0] != want {
		t.Fatalf("recorder call = %#v, want %#v", recorder.calls[0], want)
	}
}

func TestCreateCommentRejectsAutoAsANewSessionMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}
	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'invalid mode thread') RETURNING id`,
		testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Do it", "session_mode": "auto",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
