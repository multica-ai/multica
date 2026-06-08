package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createResolveCommentFixture(t *testing.T) (issueID, rootID, replyID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "comment resolve fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'root', 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&rootID); err != nil {
		t.Fatalf("create root comment: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, 'member', $3, 'reply', 'comment', $4)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, rootID).Scan(&replyID); err != nil {
		t.Fatalf("create reply comment: %v", err)
	}

	return issueID, rootID, replyID
}

func TestResolveComment_AllowsReplyComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, rootID, replyID := createResolveCommentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/comments/"+replyID+"/resolve", nil)
	req = withURLParam(req, "commentId", replyID)
	testHandler.ResolveComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ResolveComment reply: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != replyID {
		t.Fatalf("resolved id = %q, want reply %q", resp.ID, replyID)
	}
	if resp.ParentID == nil || *resp.ParentID != rootID {
		t.Fatalf("resolved reply parent_id = %v, want %q", resp.ParentID, rootID)
	}
	if resp.ResolvedAt == nil || resp.ResolvedByType == nil || resp.ResolvedByID == nil {
		t.Fatalf("resolved fields not populated: %+v", resp)
	}

	var rootResolved bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT resolved_at IS NOT NULL FROM comment WHERE id = $1
	`, rootID).Scan(&rootResolved); err != nil {
		t.Fatalf("query root resolved state: %v", err)
	}
	if rootResolved {
		t.Fatal("resolving a reply must not resolve its thread root")
	}
}

func TestUnresolveComment_AllowsReplyComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, _, replyID := createResolveCommentFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment
		SET resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $2
		WHERE id = $1
	`, replyID, testUserID); err != nil {
		t.Fatalf("seed reply resolved state: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/comments/"+replyID+"/resolve", nil)
	req = withURLParam(req, "commentId", replyID)
	testHandler.UnresolveComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnresolveComment reply: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != replyID {
		t.Fatalf("unresolved id = %q, want reply %q", resp.ID, replyID)
	}
	if resp.ResolvedAt != nil || resp.ResolvedByType != nil || resp.ResolvedByID != nil {
		t.Fatalf("resolved fields should be cleared: %+v", resp)
	}
}

func TestCreateComment_AutoUnresolvesResolvedDirectParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, rootID, replyID := createResolveCommentFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment
		SET resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $2
		WHERE id = $1
	`, replyID, testUserID); err != nil {
		t.Fatalf("seed reply resolved state: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "re-open direct parent",
		"parent_id": replyID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var replyResolved bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT resolved_at IS NOT NULL FROM comment WHERE id = $1
	`, replyID).Scan(&replyResolved); err != nil {
		t.Fatalf("query reply resolved state: %v", err)
	}
	if replyResolved {
		t.Fatal("replying to a resolved direct parent must reopen that parent comment")
	}

	var rootResolved bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT resolved_at IS NOT NULL FROM comment WHERE id = $1
	`, rootID).Scan(&rootResolved); err != nil {
		t.Fatalf("query root resolved state: %v", err)
	}
	if rootResolved {
		t.Fatal("unresolved root should not be changed by direct-parent auto reopen")
	}
}

func TestCreateComment_DoesNotAutoUnresolveResolvedRootWhenReplyingToUnresolvedReply(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID, rootID, replyID := createResolveCommentFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment
		SET resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $2
		WHERE id = $1
	`, rootID, testUserID); err != nil {
		t.Fatalf("seed root resolved state: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "reply to unresolved sibling",
		"parent_id": replyID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var rootResolved bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT resolved_at IS NOT NULL FROM comment WHERE id = $1
	`, rootID).Scan(&rootResolved); err != nil {
		t.Fatalf("query root resolved state: %v", err)
	}
	if !rootResolved {
		t.Fatal("replying to an unresolved reply must not reopen a resolved thread root")
	}
}
