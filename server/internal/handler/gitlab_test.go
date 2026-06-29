package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func TestGitLabLogin_NotConfigured(t *testing.T) {
	t.Setenv("GITLAB_URL", "")
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab", nil)
	w := httptest.NewRecorder()
	h.GitLabLogin(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_MissingSecret(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "anything")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_WrongToken(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "correct-secret")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "wrong")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_UnknownEvent(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// TestHandleGitLabIssueEvent_LabelAdd tests that an Issue Hook with the
// "agent" label on a new issue creates a Multica issue and gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelAdd(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	// Create a GitLab connection for the test workspace.
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-add",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	payload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 10, "title": "Sync me", "description": "desc", "action": "open"},
		"project": {"id": 99, "path_with_namespace": "testorg-issue-add/repo", "namespace": "testorg-issue-add"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify gitlab_issue row was created.
	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID,
		ProjectPath: "testorg-issue-add/repo",
		GlIssueIid:  10,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if issue.Title != "Sync me" {
		t.Errorf("title: got %q, want %q", issue.Title, "Sync me")
	}
}

// TestHandleGitLabIssueEvent_LabelRemove tests that removing the "agent" label
// deletes the linked Multica issue and gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelRemove(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-remove",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed: create issue+row via the add path.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "open"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-remove/repo", GlIssueIid: 11,
	})
	if err != nil {
		t.Fatalf("seed: gitlab_issue not found: %v", err)
	}
	issueID := row.IssueID

	// Remove label.
	removePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "update"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [],
		"assignees": []
	}`
	removeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(removePayload))
	removeReq.Header.Set("X-Gitlab-Token", "s")
	removeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), removeReq)

	// Multica issue should be deleted.
	if _, err := testHandler.Queries.GetIssue(ctx, issueID); err == nil {
		t.Error("multica issue should have been deleted")
	}
}

// TestHandleGitLabIssueEvent_Close tests that action=close marks the issue Done.
func TestHandleGitLabIssueEvent_Close(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-close",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "open"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-close/repo", GlIssueIid: 12,
	})

	// Close.
	closePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "close"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	closeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(closePayload))
	closeReq.Header.Set("X-Gitlab-Token", "s")
	closeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), closeReq)

	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.Status != "done" {
		t.Errorf("status: got %q, want %q", issue.Status, "done")
	}
}

// TestHandleGitLabIssueEvent_Reopen tests that action=reopen marks the issue In Progress.
func TestHandleGitLabIssueEvent_Reopen(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-reopen",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed + close.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	for _, p := range []string{
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"open"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"close"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(p))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Issue Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-reopen/repo", GlIssueIid: 13,
	})

	// Reopen.
	reopenPayload := `{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"reopen"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(reopenPayload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)

	issue, _ := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if issue.Status != "in_progress" {
		t.Errorf("status: got %q, want %q", issue.Status, "in_progress")
	}
}

// TestHandleGitLabNoteEvent_CreatesComment tests that a Note Hook creates a
// Multica comment prefixed with the GitLab username.
func TestHandleGitLabNoteEvent_CreatesComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-create",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create an issue via Issue Hook first.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":20,"title":"Note target","description":"","action":"open"},"project":{"id":200,"path_with_namespace":"testorg-note-create/repo","namespace":"testorg-note-create"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	// Fire Note Hook.
	notePayload := `{
		"object_kind": "note",
		"object_attributes": {
			"noteable_type": "Issue",
			"system": false,
			"id": 777,
			"note": "Hello from GitLab"
		},
		"project": {"path_with_namespace": "testorg-note-create/repo", "namespace": "testorg-note-create"},
		"issue": {"iid": 20},
		"user": {"username": "gitlabuser"}
	}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	// Verify comment exists with gitlab_note_id set.
	comment, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 777, Valid: true})
	if err != nil {
		t.Fatalf("comment with gitlab_note_id not found: %v", err)
	}
	if !strings.Contains(comment.Content, "**gitlabuser** (GitLab)") {
		t.Errorf("content attribution missing: %q", comment.Content)
	}
	if !strings.Contains(comment.Content, "Hello from GitLab") {
		t.Errorf("comment body missing: %q", comment.Content)
	}
}

// TestHandleGitLabNoteEvent_DuplicateSkipped tests that a Note Hook with an
// already-seen gitlab_note_id is silently skipped (echo loop prevention).
func TestHandleGitLabNoteEvent_DuplicateSkipped(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-dup",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":21,"title":"Dup note target","description":"","action":"open"},"project":{"id":201,"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	notePayload := `{"object_kind":"note","object_attributes":{"noteable_type":"Issue","system":false,"id":888,"note":"Once"},"project":{"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"issue":{"iid":21},"user":{"username":"u"}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Note Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	// Only one comment should exist for note_id 888.
	// The unique index enforces this; verify via direct query.
	var count int
	err = testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE gitlab_note_id = 888`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 comment for note 888, got %d", count)
	}
}

// TestPostCommentToGitLab_EchoLoopPrevention tests that a comment posted from
// Multica to GitLab gets a gitlab_note_id, and a subsequent Note Hook with
// that same ID is a no-op. This test mocks the GitLab API server.
func TestPostCommentToGitLab_EchoLoopPrevention(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	// Start a fake GitLab API server that records calls and returns a note ID.
	var apiCalled int
	fakeGitLab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 9999}`))
	}))
	defer fakeGitLab.Close()
	t.Setenv("GITLAB_URL", fakeGitLab.URL)
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")

	// Set up a real secretbox so token decryption works in the test.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	testHandler.GitLabBox = box
	t.Cleanup(func() { testHandler.GitLabBox = nil })

	// Seal "dummytoken" so the connection stores an encrypted token.
	sealed, err := box.Seal([]byte("dummytoken"))
	if err != nil {
		t.Fatalf("box.Seal: %v", err)
	}
	accessToken := base64.StdEncoding.EncodeToString(sealed)

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	// Create connection with a properly-sealed token.
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-echo",
		NamespaceType: "group",
		AccessToken:   accessToken,
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue via Issue Hook.
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":30,"title":"Echo test","description":"","action":"open"},"project":{"id":300,"path_with_namespace":"testorg-echo/repo","namespace":"testorg-echo"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-echo/repo", GlIssueIid: 30,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not found after seed: %v", err)
	}

	// Directly create a comment and call postCommentToGitLab.
	issue, _ := testHandler.Queries.GetIssue(ctx, row.IssueID)
	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    userUUID,
		Content:     "Hello from Multica",
		Type:        "comment",
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// postCommentToGitLab runs inline (not goroutine) for testability.
	testHandler.postCommentToGitLab(ctx, comment, issue)

	// Verify gitlab_note_id was set.
	updatedComment, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 9999, Valid: true})
	if err != nil {
		t.Fatalf("gitlab_note_id not set on comment: %v", err)
	}
	if uuidToString(updatedComment.ID) != uuidToString(comment.ID) {
		t.Error("wrong comment has gitlab_note_id")
	}

	// Now fire Note Hook with the same note ID — should be skipped.
	notePayload := `{"object_kind":"note","object_attributes":{"noteable_type":"Issue","system":false,"id":9999,"note":"Hello from Multica"},"project":{"path_with_namespace":"testorg-echo/repo","namespace":"testorg-echo"},"issue":{"iid":30},"user":{"username":"someone"}}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	// Only one comment should exist for this issue.
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&count)
	if count != 1 {
		t.Errorf("echo loop: expected 1 comment, got %d", count)
	}

	_ = apiCalled // used to confirm fake server was hit (implicit via note_id being set)
}

// TestGetGitLabIssueForIssue tests the GET /api/issues/:id/gitlab-issue endpoint.
func TestGetGitLabIssueForIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-get-issue",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue via webhook.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":40,"title":"Get me","description":"","action":"open"},"project":{"id":400,"path_with_namespace":"testorg-get-issue/repo","namespace":"testorg-get-issue"},"labels":[{"title":"agent"}],"assignees":[{"username":"getuser"}]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-get-issue/repo", GlIssueIid: 40,
	})
	if err != nil {
		t.Fatalf("seed issue not found: %v", err)
	}
	issueIDStr := uuidToString(row.IssueID)

	// GET /api/issues/:id/gitlab-issue
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueIDStr)
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+issueIDStr+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		GlIssueIID         int    `json:"gl_issue_iid"`
		ProjectPath        string `json:"project_path"`
		URL                string `json:"url"`
		GlAssigneeUsername string `json:"gl_assignee_username"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.GlIssueIID != 40 {
		t.Errorf("gl_issue_iid: got %d, want 40", resp.GlIssueIID)
	}
	if resp.ProjectPath != "testorg-get-issue/repo" {
		t.Errorf("project_path: got %q", resp.ProjectPath)
	}
	if resp.GlAssigneeUsername != "getuser" {
		t.Errorf("gl_assignee_username: got %q, want %q", resp.GlAssigneeUsername, "getuser")
	}
	if !strings.Contains(resp.URL, "testorg-get-issue/repo") {
		t.Errorf("url missing project path: %q", resp.URL)
	}
}

// TestGetGitLabIssueForIssue_NotFound tests that a non-linked issue returns 404.
func TestGetGitLabIssueForIssue_NotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}

	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	// Use a UUID that doesn't have a gitlab_issue link.
	randomUUID := pgtype.UUID{}
	if _, err := rand.Read(randomUUID.Bytes[:]); err != nil {
		t.Fatalf("generate random uuid: %v", err)
	}
	randomUUID.Valid = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidToString(randomUUID))
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+uuidToString(randomUUID)+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
