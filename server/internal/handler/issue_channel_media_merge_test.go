package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channelmedia"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMergeIssueChannelMediaDescriptionProtectsLegacyClient(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	block := channelmedia.Block(id, "diagram.png", true)
	current := channelmedia.Append("Original", block)
	attachmentID := pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}

	got := mergeIssueChannelMediaDescription(current, "Original with local edit", nil, []db.Attachment{{
		ID: attachmentID, Filename: "diagram.png", ContentType: "image/png",
	}})
	want := channelmedia.Append("Original with local edit", block)
	if got != want {
		t.Fatalf("merged description = %q, want %q", got, want)
	}
}

func TestMergeIssueChannelMediaDescriptionAllowsKnownMediaDeletion(t *testing.T) {
	const id = "33333333-3333-4333-8333-333333333333"
	current := channelmedia.Append("Original", channelmedia.Block(id, "diagram.png", true))
	attachmentID := pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}

	got := mergeIssueChannelMediaDescription(current, "Original without image", &current, []db.Attachment{{
		ID: attachmentID, Filename: "diagram.png", ContentType: "image/png",
	}})
	if got != "Original without image" {
		t.Fatalf("explicit deletion was not preserved: %q", got)
	}
}

func TestMergeIssueChannelMediaDescriptionRestoresMarkerForRetainedLink(t *testing.T) {
	const id = "55555555-5555-4555-8555-555555555555"
	current := channelmedia.Append("Original", channelmedia.Block(id, "diagram.png", true))
	attachmentID := pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}
	incoming := channelmedia.Append("Local edit", "![]("+channelmedia.DownloadPath(id)+")")

	got := mergeIssueChannelMediaDescription(current, incoming, &current, []db.Attachment{{
		ID: attachmentID, Filename: "diagram.png", ContentType: "image/png",
	}})
	if !strings.Contains(got, incoming) || !channelmedia.HasMarker(got, id) {
		t.Fatalf("retained media lost provenance: %q", got)
	}
}

func TestMergeIssueChannelMediaDescriptionDoesNotResurrectDeletedAttachment(t *testing.T) {
	const id = "66666666-6666-4666-8666-666666666666"
	current := channelmedia.Append("Original", channelmedia.Block(id, "diagram.png", true))

	got := mergeIssueChannelMediaDescription(current, "Local edit", nil, nil)
	if got != "Local edit" {
		t.Fatalf("deleted attachment was resurrected: %q", got)
	}
}

func TestRefreshUntouchedNullableIssueParamsKeepsValidatedAssigneePair(t *testing.T) {
	oldID := pgtype.UUID{Bytes: uuid.MustParse("77777777-7777-4777-8777-777777777777"), Valid: true}
	concurrentID := pgtype.UUID{Bytes: uuid.MustParse("88888888-8888-4888-8888-888888888888"), Valid: true}
	params := db.UpdateIssueParams{
		AssigneeType: pgtype.Text{String: "member", Valid: true},
		AssigneeID:   oldID,
	}
	current := db.Issue{
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   concurrentID,
	}

	refreshUntouchedNullableIssueParams(&params, current, map[string]json.RawMessage{
		"assignee_id": json.RawMessage(`"77777777-7777-4777-8777-777777777777"`),
	})

	if params.AssigneeType.String != "member" || params.AssigneeID != oldID {
		t.Fatalf("validated assignee pair was recombined: type=%#v id=%#v", params.AssigneeType, params.AssigneeID)
	}
}

func TestUpdateIssueMergesChannelMediaAppendedAfterEditorBase(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	ctx := context.Background()

	create := httptest.NewRecorder()
	createReq := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Channel media description merge",
		"description": "Original",
		"status":      "todo",
		"priority":    "none",
	})
	testHandler.CreateIssue(create, createReq)
	if create.Code != http.StatusCreated {
		t.Fatalf("create issue: status %d: %s", create.Code, create.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	attachmentID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, issue_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'diagram.png', $5, 'image/png', 3)
	`, attachmentID, testWorkspaceID, created.ID, testUserID, "https://cdn.example.test/"+attachmentID); err != nil {
		t.Fatalf("insert channel attachment: %v", err)
	}
	block := channelmedia.Block(attachmentID, "diagram.png", true)
	remoteDescription := channelmedia.Append("Original", block)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = $1, updated_at = now() WHERE id = $2`, remoteDescription, created.ID); err != nil {
		t.Fatalf("append remote channel media: %v", err)
	}

	update := httptest.NewRecorder()
	updateReq := newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
		"description":      "Original with local edit",
		"description_base": "Original",
	})
	updateReq = withURLParam(updateReq, "id", created.ID)
	testHandler.UpdateIssue(update, updateReq)
	if update.Code != http.StatusOK {
		t.Fatalf("update issue: status %d: %s", update.Code, update.Body.String())
	}
	var merged IssueResponse
	if err := json.NewDecoder(update.Body).Decode(&merged); err != nil {
		t.Fatalf("decode merged issue: %v", err)
	}
	if merged.Description == nil || !strings.Contains(*merged.Description, "Original with local edit") || !strings.Contains(*merged.Description, channelmedia.DownloadPath(attachmentID)) {
		t.Fatalf("merged description = %#v", merged.Description)
	}

	// Once the client has adopted the media-bearing description as its base,
	// removing the image is an explicit edit and must remain possible.
	remove := httptest.NewRecorder()
	removeReq := newRequest(http.MethodPut, "/api/issues/"+created.ID, map[string]any{
		"description":      "Original with local edit",
		"description_base": *merged.Description,
	})
	removeReq = withURLParam(removeReq, "id", created.ID)
	testHandler.UpdateIssue(remove, removeReq)
	if remove.Code != http.StatusOK {
		t.Fatalf("remove image: status %d: %s", remove.Code, remove.Body.String())
	}
	var removed IssueResponse
	if err := json.NewDecoder(remove.Body).Decode(&removed); err != nil {
		t.Fatalf("decode image removal: %v", err)
	}
	if removed.Description == nil || *removed.Description != "Original with local edit" {
		t.Fatalf("description after explicit image removal = %#v", removed.Description)
	}
}

func TestBatchUpdateIssuePreservesMarkedChannelMedia(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	ctx := context.Background()

	create := httptest.NewRecorder()
	createReq := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Batch channel media merge",
		"description": "Original",
		"status":      "todo",
		"priority":    "none",
	})
	testHandler.CreateIssue(create, createReq)
	if create.Code != http.StatusCreated {
		t.Fatalf("create issue: status %d: %s", create.Code, create.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	attachmentID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, issue_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'diagram.png', $5, 'image/png', 3)
	`, attachmentID, testWorkspaceID, created.ID, testUserID, "https://cdn.example.test/"+attachmentID); err != nil {
		t.Fatalf("insert channel attachment: %v", err)
	}
	block := channelmedia.Block(attachmentID, "diagram.png", true)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = $1 WHERE id = $2`, channelmedia.Append("Original", block), created.ID); err != nil {
		t.Fatalf("append channel media: %v", err)
	}

	update := httptest.NewRecorder()
	updateReq := newRequest(http.MethodPut, "/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{created.ID},
		"updates": map[string]any{
			"description": "Batch edit",
		},
	})
	testHandler.BatchUpdateIssues(update, updateReq)
	if update.Code != http.StatusOK {
		t.Fatalf("batch update: status %d: %s", update.Code, update.Body.String())
	}

	var description string
	if err := testPool.QueryRow(ctx, `SELECT description FROM issue WHERE id = $1`, created.ID).Scan(&description); err != nil {
		t.Fatalf("load batch-updated description: %v", err)
	}
	want := channelmedia.Append("Batch edit", block)
	if description != want {
		t.Fatalf("batch-updated description = %q, want %q", description, want)
	}
}

func TestDescriptionUpdateLockSerializesLaterChannelMediaAppend(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	ctx := context.Background()
	var issueID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			creator_type, creator_id, number
		) VALUES ($1, 'Description/media lock race', 'Original', 'todo', 'none', 'member', $2,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	workspaceID := pgtype.UUID{Bytes: uuid.MustParse(testWorkspaceID), Valid: true}

	descriptionTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin description transaction: %v", err)
	}
	defer descriptionTx.Rollback(ctx)
	if _, err := db.New(testPool).WithTx(descriptionTx).LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: issueID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("lock issue for description update: %v", err)
	}
	if _, err := descriptionTx.Exec(ctx, `UPDATE issue SET description = 'Local edit' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("write local description under lock: %v", err)
	}

	bindTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin media bind transaction: %v", err)
	}
	defer bindTx.Rollback(ctx)
	var bindPID int32
	if err := bindTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&bindPID); err != nil {
		t.Fatalf("load bind backend pid: %v", err)
	}
	bindResult := make(chan error, 1)
	go func() {
		_, lockErr := db.New(testPool).WithTx(bindTx).LockIssueForChannelMediaBind(context.Background(), db.LockIssueForChannelMediaBindParams{
			ID: issueID, WorkspaceID: workspaceID,
		})
		bindResult <- lockErr
	}()

	blocked := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if err := testPool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, bindPID).Scan(&blocked); err != nil {
			t.Fatalf("inspect media lock wait: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("channel media bind did not block behind description update")
	}
	if err := descriptionTx.Commit(ctx); err != nil {
		t.Fatalf("commit description update: %v", err)
	}
	select {
	case err := <-bindResult:
		if err != nil {
			t.Fatalf("acquire media bind lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("media bind stayed blocked after description commit")
	}

	const attachmentID = "44444444-4444-4444-8444-444444444444"
	block := channelmedia.Block(attachmentID, "diagram.png", true)
	if _, err := db.New(testPool).WithTx(bindTx).MaterializeIssueChannelMediaMarkdown(ctx, db.MaterializeIssueChannelMediaMarkdownParams{
		ID: issueID, WorkspaceID: workspaceID, Markdown: pgtype.Text{String: block, Valid: true},
	}); err != nil {
		t.Fatalf("append channel media after description commit: %v", err)
	}
	if err := bindTx.Commit(ctx); err != nil {
		t.Fatalf("commit media append: %v", err)
	}

	var description string
	if err := testPool.QueryRow(ctx, `SELECT description FROM issue WHERE id = $1`, issueID).Scan(&description); err != nil {
		t.Fatalf("load final description: %v", err)
	}
	want := channelmedia.Append("Local edit", block)
	if description != want {
		t.Fatalf("final description = %q, want %q", description, want)
	}
}
