package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errInjectedIssueAttachmentLink = errors.New("injected issue attachment link failure")

type failIssueAttachmentLinkTxStarter struct {
	inner txStarter
}

func (s failIssueAttachmentLinkTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failIssueAttachmentLinkTx{Tx: tx}, nil
}

type failIssueAttachmentLinkTx struct {
	pgx.Tx
}

func (tx *failIssueAttachmentLinkTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "LinkAttachmentsToIssue") {
		return failingIssueAttachmentLinkRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

type failingIssueAttachmentLinkRow struct{}

func (failingIssueAttachmentLinkRow) Scan(...any) error {
	return errInjectedIssueAttachmentLink
}

type pauseIssueAttachmentLinkTxStarter struct {
	inner   txStarter
	reached chan<- struct{}
	release <-chan struct{}
}

func (s pauseIssueAttachmentLinkTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pauseIssueAttachmentLinkTx{
		Tx:      tx,
		reached: s.reached,
		release: s.release,
	}, nil
}

type pauseIssueAttachmentLinkTx struct {
	pgx.Tx
	reached chan<- struct{}
	release <-chan struct{}
}

func (tx *pauseIssueAttachmentLinkTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "LinkAttachmentsToIssue") {
		tx.reached <- struct{}{}
		select {
		case <-tx.release:
		case <-ctx.Done():
			return failingIssueAttachmentLinkRow{}
		}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func insertWorkflowTestIssue(t *testing.T, title string, number int) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("insert workflow issue: %v", err)
	}
	return id
}

func TestRevisionConflictsPreserveLatestIssueAndComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "revision original", int(time.Now().UnixNano()%100000)+8_750_000)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'comment original')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert revision comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	updateIssue := func(title, base string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
			"title": title, "title_base": base,
		}), "id", issueID)
		testHandler.UpdateIssue(w, req)
		return w
	}
	if w := updateIssue("revision latest", "revision original"); w.Code != http.StatusOK {
		t.Fatalf("first issue update = %d: %s", w.Code, w.Body.String())
	}
	if w := updateIssue("revision latest", "revision latest"); w.Code != http.StatusOK {
		t.Fatalf("no-op issue update = %d: %s", w.Code, w.Body.String())
	}
	if w := updateIssue("revision stale overwrite", "revision original"); w.Code != http.StatusConflict {
		t.Fatalf("stale issue update = %d, want 409: %s", w.Code, w.Body.String())
	}
	var title string
	var issueRevision int64
	if err := testPool.QueryRow(ctx, `SELECT title, revision FROM issue WHERE id = $1`, issueID).Scan(&title, &issueRevision); err != nil {
		t.Fatalf("reload revision issue: %v", err)
	}
	if title != "revision latest" || issueRevision != 2 {
		t.Fatalf("issue after stale write = (%q, %d), want latest/2", title, issueRevision)
	}
	legacyIssue := httptest.NewRecorder()
	testHandler.UpdateIssue(legacyIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"title": "legacy issue update",
	}), "id", issueID))
	if legacyIssue.Code != http.StatusOK {
		t.Fatalf("legacy issue update = %d: %s", legacyIssue.Code, legacyIssue.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT title, revision FROM issue WHERE id = $1`, issueID).Scan(&title, &issueRevision); err != nil {
		t.Fatalf("reload legacy issue update: %v", err)
	}
	if title != "legacy issue update" || issueRevision != 3 {
		t.Fatalf("legacy issue update = (%q, %d), want legacy issue update/3", title, issueRevision)
	}

	updateComment := func(content, base string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
			"content": content, "content_base": base,
		}), "commentId", commentID)
		testHandler.UpdateComment(w, req)
		return w
	}
	if w := updateComment("comment latest", "comment original"); w.Code != http.StatusOK {
		t.Fatalf("first comment update = %d: %s", w.Code, w.Body.String())
	}
	if w := updateComment("comment latest", "comment latest"); w.Code != http.StatusOK {
		t.Fatalf("no-op comment update = %d: %s", w.Code, w.Body.String())
	}
	if w := updateComment("comment stale overwrite", "comment original"); w.Code != http.StatusConflict {
		t.Fatalf("stale comment update = %d, want 409: %s", w.Code, w.Body.String())
	}
	var content string
	var commentRevision int64
	if err := testPool.QueryRow(ctx, `SELECT content, revision FROM comment WHERE id = $1`, commentID).Scan(&content, &commentRevision); err != nil {
		t.Fatalf("reload revision comment: %v", err)
	}
	if content != "comment latest" || commentRevision != 2 {
		t.Fatalf("comment after stale write = (%q, %d), want latest/2", content, commentRevision)
	}

	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, issue_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		)
		VALUES ($1, $2, 'member', $3, 'revision.txt', '/revision.txt', 'text/plain', 1)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert revision attachment: %v", err)
	}
	updateAttachments := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
			"content":        "comment latest",
			"attachment_ids": []string{attachmentID},
			"content_base":   "comment latest",
		}), "commentId", commentID)
		testHandler.UpdateComment(w, req)
		return w
	}
	if w := updateAttachments(); w.Code != http.StatusOK {
		t.Fatalf("attachment-only comment update = %d: %s", w.Code, w.Body.String())
	}
	if w := updateAttachments(); w.Code != http.StatusOK {
		t.Fatalf("attachment no-op comment update = %d: %s", w.Code, w.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT revision FROM comment WHERE id = $1`, commentID).Scan(&commentRevision); err != nil {
		t.Fatalf("reload attachment comment revision: %v", err)
	}
	if commentRevision != 3 {
		t.Fatalf("comment revision after attachment update = %d, want 3", commentRevision)
	}
	legacyComment := httptest.NewRecorder()
	testHandler.UpdateComment(legacyComment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content": "legacy comment update",
	}), "commentId", commentID))
	if legacyComment.Code != http.StatusOK {
		t.Fatalf("legacy comment update = %d: %s", legacyComment.Code, legacyComment.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT content, revision FROM comment WHERE id = $1`, commentID).Scan(&content, &commentRevision); err != nil {
		t.Fatalf("reload legacy comment update: %v", err)
	}
	if content != "legacy comment update" || commentRevision != 4 {
		t.Fatalf("legacy comment update = (%q, %d), want legacy comment update/4", content, commentRevision)
	}
}

func TestTextBaselinesIgnoreUnrelatedAggregateRevisionChanges(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "baseline title", int(time.Now().UnixNano()%100000)+8_755_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = 'baseline description' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("seed issue description: %v", err)
	}
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'baseline comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert baseline comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// An unrelated priority write advances the aggregate owner revision.
	priority := httptest.NewRecorder()
	testHandler.UpdateIssue(priority, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"priority": "high",
	}), "id", issueID))
	if priority.Code != http.StatusOK {
		t.Fatalf("unrelated issue update = %d: %s", priority.Code, priority.Body.String())
	}

	text := httptest.NewRecorder()
	testHandler.UpdateIssue(text, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"title":            "edited title",
		"title_base":       "baseline title",
		"description":      "edited description",
		"description_base": "baseline description",
	}), "id", issueID))
	if text.Code != http.StatusOK {
		t.Fatalf("text update after unrelated revision = %d: %s", text.Code, text.Body.String())
	}
	staleDescription := httptest.NewRecorder()
	testHandler.UpdateIssue(staleDescription, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"description":      "stale description overwrite",
		"description_base": "baseline description",
	}), "id", issueID))
	if staleDescription.Code != http.StatusConflict {
		t.Fatalf("true issue description conflict = %d, want 409: %s", staleDescription.Code, staleDescription.Body.String())
	}

	// A reaction/resolve-style aggregate bump on the comment must likewise not
	// reject a body edit whose content baseline is still current.
	if _, err := testPool.Exec(ctx, `UPDATE comment SET revision = revision + 1 WHERE id = $1`, commentID); err != nil {
		t.Fatalf("bump comment revision: %v", err)
	}
	comment := httptest.NewRecorder()
	testHandler.UpdateComment(comment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content":      "edited comment",
		"content_base": "baseline comment",
	}), "commentId", commentID))
	if comment.Code != http.StatusOK {
		t.Fatalf("comment edit after unrelated revision = %d: %s", comment.Code, comment.Body.String())
	}

	stale := httptest.NewRecorder()
	testHandler.UpdateComment(stale, withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content":      "stale overwrite",
		"content_base": "baseline comment",
	}), "commentId", commentID))
	if stale.Code != http.StatusConflict {
		t.Fatalf("true comment content conflict = %d, want 409: %s", stale.Code, stale.Body.String())
	}
}

func TestConcurrentRevisionWritesHaveExactlyOneWinner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "concurrent revision", int(time.Now().UnixNano()%100000)+8_760_000)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'concurrent original')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert concurrent comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	assertOneWinner := func(run func(value string) int) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan int, 2)
		for _, value := range []string{"writer-a", "writer-b"} {
			go func() {
				<-start
				results <- run(value)
			}()
		}
		close(start)
		okCount, conflictCount := 0, 0
		for range 2 {
			switch <-results {
			case http.StatusOK:
				okCount++
			case http.StatusConflict:
				conflictCount++
			}
		}
		if okCount != 1 || conflictCount != 1 {
			t.Fatalf("concurrent outcomes = ok:%d conflict:%d, want 1/1", okCount, conflictCount)
		}
	}

	assertOneWinner(func(title string) int {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
			"title": title, "title_base": "concurrent revision",
		}), "id", issueID)
		testHandler.UpdateIssue(w, req)
		return w.Code
	})

	assertOneWinner(func(content string) int {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
			"content": content, "content_base": "concurrent original",
		}), "commentId", commentID)
		testHandler.UpdateComment(w, req)
		return w.Code
	})

	var issueRevision, commentRevision int64
	if err := testPool.QueryRow(ctx, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&issueRevision); err != nil {
		t.Fatalf("reload concurrent issue: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT revision FROM comment WHERE id = $1`, commentID).Scan(&commentRevision); err != nil {
		t.Fatalf("reload concurrent comment: %v", err)
	}
	// The winning comment edit is semantic issue activity, so it advances the
	// parent issue revision once in addition to the winning issue title edit.
	if issueRevision != 3 || commentRevision != 2 {
		t.Fatalf("concurrent revisions = issue:%d comment:%d, want 3/2", issueRevision, commentRevision)
	}
}

func TestConcurrentCommentRevisionConflictCancelsTaskBatchOnce(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := createCommentDeliveryFixture(t, "revision task side effects")
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1
	`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign revision test issue: %v", err)
	}

	cancelEvents := make(chan events.Event, 4)
	testHandler.Bus.Subscribe(protocol.EventTaskCancelled, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if ok && payload["issue_id"] == fixture.issueID {
			cancelEvents <- event
		}
	})

	start := make(chan struct{})
	results := make(chan int, 2)
	for _, content := range []string{"concurrent edit a", "concurrent edit b"} {
		go func() {
			<-start
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+fixture.commentID[2], map[string]any{
				"content":      content,
				"content_base": fixture.content[2],
			}), "commentId", fixture.commentID[2])
			testHandler.UpdateComment(w, req)
			results <- w.Code
		}()
	}
	close(start)
	okCount, conflictCount := 0, 0
	for range 2 {
		switch <-results {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent comment outcomes = ok:%d conflict:%d, want 1/1", okCount, conflictCount)
	}

	var originalStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&originalStatus); err != nil {
		t.Fatalf("load original task: %v", err)
	}
	if originalStatus != "cancelled" {
		t.Fatalf("original task status = %q, want cancelled", originalStatus)
	}
	if got := len(cancelEvents); got != 1 {
		t.Fatalf("task cancellation events = %d, want exactly one winning edit side effect", got)
	}
	var activeCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
	`, fixture.issueID).Scan(&activeCount); err != nil {
		t.Fatalf("count active replacement tasks: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active replacement tasks = %d, want one batch from the winning edit", activeCount)
	}
}

func TestNoOpIssueUpdateDoesNotAdvanceRevisionOrUpdatedAt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "no-op issue update", int(time.Now().UnixNano()%100000)+8_768_000)
	oldUpdatedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET updated_at = $2 WHERE id = $1`, issueID, oldUpdatedAt); err != nil {
		t.Fatalf("seed old issue timestamp: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"title":             "no-op issue update",
		"expected_revision": 1,
	}), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("no-op update = %d: %s", w.Code, w.Body.String())
	}
	var revision int64
	var updatedAt time.Time
	if err := testPool.QueryRow(ctx, `SELECT revision, updated_at FROM issue WHERE id = $1`, issueID).Scan(&revision, &updatedAt); err != nil {
		t.Fatalf("load no-op issue: %v", err)
	}
	if revision != 1 || !updatedAt.Equal(oldUpdatedAt) {
		t.Fatalf("no-op issue = revision %d, updated_at %s; want 1 and %s", revision, updatedAt, oldUpdatedAt)
	}
}

func TestNoOpIssueWithAttachmentPublishesFreshOwnerBeforeAttachmentEvent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "ordered attachment events", int(time.Now().UnixNano()%100000)+8_770_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET updated_at = '2000-01-01T00:00:00Z' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("seed old issue timestamp: %v", err)
	}
	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		)
		VALUES ($1, 'member', $2, 'ordered.txt', '/ordered.txt', 'text/plain', 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert unbound attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, attachmentID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	h := *testHandler
	h.Bus = events.New()
	var ordered []events.Event
	h.Bus.SubscribeAll(func(event events.Event) {
		if event.Type == protocol.EventIssueUpdated || event.Type == protocol.EventIssueAttachmentsChanged {
			ordered = append(ordered, event)
		}
	})

	w := httptest.NewRecorder()
	h.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"title":             "ordered attachment events",
		"attachment_ids":    []string{attachmentID},
		"expected_revision": 1,
	}), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("combined update = %d: %s", w.Code, w.Body.String())
	}
	if len(ordered) != 2 || ordered[0].Type != protocol.EventIssueUpdated || ordered[1].Type != protocol.EventIssueAttachmentsChanged {
		t.Fatalf("event order = %#v, want issue:updated then issue_attachments:changed", ordered)
	}
	updatedPayload, ok := ordered[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("updated payload = %#v", ordered[0].Payload)
	}
	updatedIssue, ok := updatedPayload["issue"].(IssueResponse)
	if !ok {
		t.Fatalf("updated issue = %#v", updatedPayload["issue"])
	}
	if updatedIssue.Revision != 2 {
		t.Fatalf("no-op owner revision = %d, want 2", updatedIssue.Revision)
	}
	responseUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedIssue.UpdatedAt)
	if err != nil {
		t.Fatalf("parse response updated_at %q: %v", updatedIssue.UpdatedAt, err)
	}
	if !responseUpdatedAt.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("response updated_at = %s, want refreshed timestamp", responseUpdatedAt)
	}
	var updatedAt time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&updatedAt); err != nil {
		t.Fatalf("load no-op updated_at: %v", err)
	}
	if !updatedAt.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("no-op updated_at = %s, want refreshed timestamp", updatedAt)
	}
	attachmentPayload, ok := ordered[1].Payload.(map[string]any)
	if !ok || attachmentPayload["issue_revision"] != updatedIssue.Revision {
		t.Fatalf("event revisions = updated:%#v attachment:%#v", updatedIssue.Revision, attachmentPayload["issue_revision"])
	}
}

func TestIssueUpdateAndAttachmentBindingExcludeInterleavingMutation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "atomic attachment binding", int(time.Now().UnixNano()%100000)+8_772_000)
	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		)
		VALUES ($1, 'member', $2, 'atomic.txt', '/atomic.txt', 'text/plain', 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert atomic attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	reachedLink := make(chan struct{}, 1)
	releaseLink := make(chan struct{})
	defer func() {
		select {
		case <-releaseLink:
		default:
			close(releaseLink)
		}
	}()
	h := *testHandler
	h.TxStarter = pauseIssueAttachmentLinkTxStarter{
		inner:   testHandler.TxStarter,
		reached: reachedLink,
		release: releaseLink,
	}
	handlerDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
			"title":             "atomic attachment binding",
			"attachment_ids":    []string{attachmentID},
			"expected_revision": 1,
		}), "id", issueID))
		handlerDone <- w
	}()

	select {
	case <-reachedLink:
	case <-time.After(5 * time.Second):
		t.Fatal("issue update did not reach the attachment link")
	}

	concurrentDone := make(chan error, 1)
	go func() {
		_, execErr := testPool.Exec(ctx, `
			/* issue_revision_interleaving */
			UPDATE issue
			SET priority = 'high', revision = revision + 1
			WHERE id = $1
		`, issueID)
		concurrentDone <- execErr
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%issue_revision_interleaving%'
			)
		`).Scan(&waiting); err != nil {
			t.Fatalf("observe blocked concurrent mutation: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent mutation was not blocked by the issue update transaction")
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(releaseLink)
	response := <-handlerDone
	if response.Code != http.StatusOK {
		t.Fatalf("combined update = %d: %s", response.Code, response.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
		t.Fatalf("decode combined update: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("combined update revision = %d, want 2", updated.Revision)
	}
	if err := <-concurrentDone; err != nil {
		t.Fatalf("concurrent mutation after commit: %v", err)
	}

	var linkedIssueID, priority string
	var revision int64
	if err := testPool.QueryRow(ctx, `
		SELECT a.issue_id, i.priority, i.revision
		FROM attachment AS a
		JOIN issue AS i ON i.id = a.issue_id
		WHERE a.id = $1
	`, attachmentID).Scan(&linkedIssueID, &priority, &revision); err != nil {
		t.Fatalf("load atomic result: %v", err)
	}
	if linkedIssueID != issueID || priority != "high" || revision != 3 {
		t.Fatalf("atomic result = issue %s, priority %s, revision %d; want %s/high/3", linkedIssueID, priority, revision, issueID)
	}
}

func TestIssueUpdateRollsBackWhenAttachmentBindingFails(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "attachment rollback original", int(time.Now().UnixNano()%100000)+8_774_000)
	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		)
		VALUES ($1, 'member', $2, 'rollback.txt', '/rollback.txt', 'text/plain', 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert rollback attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	h := *testHandler
	h.TxStarter = failIssueAttachmentLinkTxStarter{inner: testHandler.TxStarter}
	w := httptest.NewRecorder()
	h.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"title":          "attachment rollback changed",
		"attachment_ids": []string{attachmentID},
	}), "id", issueID))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("injected link failure = %d, want 500: %s", w.Code, w.Body.String())
	}

	var title string
	var revision int64
	if err := testPool.QueryRow(ctx, `SELECT title, revision FROM issue WHERE id = $1`, issueID).Scan(&title, &revision); err != nil {
		t.Fatalf("load rolled-back issue: %v", err)
	}
	var linked bool
	if err := testPool.QueryRow(ctx, `SELECT issue_id IS NOT NULL FROM attachment WHERE id = $1`, attachmentID).Scan(&linked); err != nil {
		t.Fatalf("load rolled-back attachment: %v", err)
	}
	if title != "attachment rollback original" || revision != 1 || linked {
		t.Fatalf("rollback result = title %q, revision %d, linked %t", title, revision, linked)
	}
}

func TestAuxiliaryVisibleMutationsAdvanceOwnerRevisionExactlyOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := insertWorkflowTestIssue(t, "revision auxiliary projections", int(time.Now().UnixNano()%100000)+8_775_000)
	issueUUID, _ := util.ParseUUID(issueID)
	workspaceUUID, _ := util.ParseUUID(testWorkspaceID)
	userUUID, _ := util.ParseUUID(testUserID)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'auxiliary revision comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert auxiliary comment: %v", err)
	}
	commentUUID, _ := util.ParseUUID(commentID)
	labelID := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_label (id, workspace_id, resource_type, name, color)
		VALUES ($1, $2, 'issue', $3, '#3b82f6')
	`, labelID, testWorkspaceID, "workflow-aux-"+labelID.String()); err != nil {
		t.Fatalf("insert auxiliary label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM attachment WHERE issue_id = $1 OR comment_id = $2`, issueID, commentID)
		testPool.Exec(ctx, `DELETE FROM comment_reaction WHERE comment_id = $1`, commentID)
		testPool.Exec(ctx, `DELETE FROM issue_reaction WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue_to_label WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue_label WHERE id = $1`, labelID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	labelParams := db.AttachLabelToIssueParams{IssueID: issueUUID, LabelID: pgtype.UUID{Bytes: labelID, Valid: true}, WorkspaceID: workspaceUUID}
	attached, err := testHandler.Queries.AttachLabelToIssue(ctx, labelParams)
	if err != nil || !attached.Changed || attached.IssueRevision != 2 {
		t.Fatalf("attach label = (%+v, %v), want changed revision 2", attached, err)
	}
	attached, err = testHandler.Queries.AttachLabelToIssue(ctx, labelParams)
	if err != nil || attached.Changed || attached.IssueRevision != 0 {
		t.Fatalf("duplicate label attach = (%+v, %v), want no-op", attached, err)
	}
	labelsResponse := httptest.NewRecorder()
	testHandler.ListLabelsForIssue(labelsResponse, withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/labels", nil), "id", issueID))
	if labelsResponse.Code != http.StatusOK {
		t.Fatalf("list issue labels = %d: %s", labelsResponse.Code, labelsResponse.Body.String())
	}
	var labelPayload struct {
		IssueRevision int64 `json:"issue_revision"`
	}
	if err := json.Unmarshal(labelsResponse.Body.Bytes(), &labelPayload); err != nil || labelPayload.IssueRevision != 2 {
		t.Fatalf("list issue labels revision = (%d, %v), want 2", labelPayload.IssueRevision, err)
	}
	detached, err := testHandler.Queries.DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams(labelParams))
	if err != nil || !detached.Changed || detached.IssueRevision != 3 {
		t.Fatalf("detach label = (%+v, %v), want changed revision 3", detached, err)
	}
	detached, err = testHandler.Queries.DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams(labelParams))
	if err != nil || detached.Changed || detached.IssueRevision != 0 {
		t.Fatalf("duplicate label detach = (%+v, %v), want no-op", detached, err)
	}

	issueReaction := db.AddIssueReactionParams{IssueID: issueUUID, WorkspaceID: workspaceUUID, ActorType: "member", ActorID: userUUID, Emoji: "eyes"}
	addedIssueReaction, err := testHandler.Queries.AddIssueReaction(ctx, issueReaction)
	if err != nil || addedIssueReaction.IssueRevision != 4 {
		t.Fatalf("add issue reaction = (%+v, %v), want revision 4", addedIssueReaction, err)
	}
	addedIssueReaction, err = testHandler.Queries.AddIssueReaction(ctx, issueReaction)
	if err != nil || addedIssueReaction.IssueRevision != 0 {
		t.Fatalf("duplicate issue reaction = (%+v, %v), want no-op", addedIssueReaction, err)
	}
	removedIssueReaction, err := testHandler.Queries.RemoveIssueReaction(ctx, db.RemoveIssueReactionParams{
		IssueID: issueUUID, ActorType: "member", ActorID: userUUID, Emoji: "eyes",
	})
	if err != nil || !removedIssueReaction.Changed || removedIssueReaction.IssueRevision != 5 {
		t.Fatalf("remove issue reaction = (%+v, %v), want revision 5", removedIssueReaction, err)
	}
	removedIssueReaction, err = testHandler.Queries.RemoveIssueReaction(ctx, db.RemoveIssueReactionParams{
		IssueID: issueUUID, ActorType: "member", ActorID: userUUID, Emoji: "eyes",
	})
	if err != nil || removedIssueReaction.Changed || removedIssueReaction.IssueRevision != 0 {
		t.Fatalf("duplicate issue reaction removal = (%+v, %v), want no-op", removedIssueReaction, err)
	}

	commentReaction := db.AddReactionParams{CommentID: commentUUID, WorkspaceID: workspaceUUID, ActorType: "member", ActorID: userUUID, Emoji: "thumbs_up"}
	addedCommentReaction, err := testHandler.Queries.AddReaction(ctx, commentReaction)
	if err != nil || addedCommentReaction.CommentRevision != 2 {
		t.Fatalf("add comment reaction = (%+v, %v), want revision 2", addedCommentReaction, err)
	}
	addedCommentReaction, err = testHandler.Queries.AddReaction(ctx, commentReaction)
	if err != nil || addedCommentReaction.CommentRevision != 0 {
		t.Fatalf("duplicate comment reaction = (%+v, %v), want no-op", addedCommentReaction, err)
	}
	removedCommentReaction, err := testHandler.Queries.RemoveReaction(ctx, db.RemoveReactionParams{
		CommentID: commentUUID, ActorType: "member", ActorID: userUUID, Emoji: "thumbs_up",
	})
	if err != nil || !removedCommentReaction.Changed || removedCommentReaction.CommentRevision != 3 {
		t.Fatalf("remove comment reaction = (%+v, %v), want revision 3", removedCommentReaction, err)
	}
	removedCommentReaction, err = testHandler.Queries.RemoveReaction(ctx, db.RemoveReactionParams{
		CommentID: commentUUID, ActorType: "member", ActorID: userUUID, Emoji: "thumbs_up",
	})
	if err != nil || removedCommentReaction.Changed || removedCommentReaction.CommentRevision != 0 {
		t.Fatalf("duplicate comment reaction removal = (%+v, %v), want no-op", removedCommentReaction, err)
	}

	attachmentID := uuid.New()
	createdAttachment, err := testHandler.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID: pgtype.UUID{Bytes: attachmentID, Valid: true}, WorkspaceID: workspaceUUID,
		IssueID: issueUUID, CommentID: commentUUID, UploaderType: "member", UploaderID: userUUID,
		Filename: "aux.txt", Url: "/aux.txt", ContentType: "text/plain", SizeBytes: 1,
	})
	if err != nil || createdAttachment.IssueRevision != 6 || createdAttachment.CommentRevision != 4 {
		t.Fatalf("create attachment = (%+v, %v), want issue/comment revisions 6/4", createdAttachment, err)
	}
	deletedAttachment, err := testHandler.Queries.DeleteAttachment(ctx, db.DeleteAttachmentParams{
		ID: pgtype.UUID{Bytes: attachmentID, Valid: true}, WorkspaceID: workspaceUUID,
	})
	if err != nil || !deletedAttachment.Changed || deletedAttachment.IssueRevision != 7 || deletedAttachment.CommentRevision != 5 {
		t.Fatalf("delete attachment = (%+v, %v), want issue/comment revisions 7/5", deletedAttachment, err)
	}

	unboundAttachmentID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := testHandler.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID: unboundAttachmentID, WorkspaceID: workspaceUUID,
		UploaderType: "member", UploaderID: userUUID,
		Filename: "linked.txt", Url: "/linked.txt", ContentType: "text/plain", SizeBytes: 1,
	}); err != nil {
		t.Fatalf("create unbound attachment: %v", err)
	}
	linkedAttachment, err := testHandler.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID: issueUUID, WorkspaceID: workspaceUUID, AttachmentIds: []pgtype.UUID{unboundAttachmentID}, BumpRevision: true,
	})
	if err != nil || linkedAttachment.LinkedCount != 1 || linkedAttachment.IssueRevision != 8 {
		t.Fatalf("link attachment = (%+v, %v), want one link and issue revision 8", linkedAttachment, err)
	}
	linkedAttachment, err = testHandler.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID: issueUUID, WorkspaceID: workspaceUUID, AttachmentIds: []pgtype.UUID{unboundAttachmentID}, BumpRevision: true,
	})
	if err != nil || linkedAttachment.LinkedCount != 0 || linkedAttachment.IssueRevision != 0 {
		t.Fatalf("duplicate attachment link = (%+v, %v), want no-op", linkedAttachment, err)
	}

	metadataParams := db.SetIssueMetadataKeyParams{
		Key: "revision-test", Value: []byte(`"value"`), ID: issueUUID, WorkspaceID: workspaceUUID,
	}
	metadataIssue, err := testHandler.Queries.SetIssueMetadataKey(ctx, metadataParams)
	if err != nil || metadataIssue.Revision != 9 {
		t.Fatalf("set metadata = (%+v, %v), want revision 9", metadataIssue, err)
	}
	metadataIssue, err = testHandler.Queries.SetIssueMetadataKey(ctx, metadataParams)
	if err != nil || metadataIssue.Revision != 9 {
		t.Fatalf("duplicate metadata set = (%+v, %v), want unchanged revision 9", metadataIssue, err)
	}
	metadataIssue, err = testHandler.Queries.DeleteIssueMetadataKey(ctx, db.DeleteIssueMetadataKeyParams{
		Key: metadataParams.Key, ID: issueUUID, WorkspaceID: workspaceUUID,
	})
	if err != nil || metadataIssue.Revision != 10 {
		t.Fatalf("delete metadata = (%+v, %v), want revision 10", metadataIssue, err)
	}

	propertyParams := db.SetIssuePropertyValueParams{
		Key: "revision-test", Value: []byte(`42`), ID: issueUUID, WorkspaceID: workspaceUUID,
	}
	propertyIssue, err := testHandler.Queries.SetIssuePropertyValue(ctx, propertyParams)
	if err != nil || propertyIssue.Revision != 11 {
		t.Fatalf("set property = (%+v, %v), want revision 11", propertyIssue, err)
	}
	propertyIssue, err = testHandler.Queries.SetIssuePropertyValue(ctx, propertyParams)
	if err != nil || propertyIssue.Revision != 11 {
		t.Fatalf("duplicate property set = (%+v, %v), want unchanged revision 11", propertyIssue, err)
	}
	propertyIssue, err = testHandler.Queries.DeleteIssuePropertyValue(ctx, db.DeleteIssuePropertyValueParams{
		Key: propertyParams.Key, ID: issueUUID, WorkspaceID: workspaceUUID,
	})
	if err != nil || propertyIssue.Revision != 12 {
		t.Fatalf("delete property = (%+v, %v), want revision 12", propertyIssue, err)
	}

	resolved, err := testHandler.Queries.ResolveComment(ctx, db.ResolveCommentParams{
		ID: commentUUID, ResolvedByType: pgtype.Text{String: "member", Valid: true}, ResolvedByID: userUUID,
	})
	if err != nil || resolved.Revision != 6 {
		t.Fatalf("resolve comment = (%+v, %v), want revision 6", resolved, err)
	}
	resolved, err = testHandler.Queries.ResolveComment(ctx, db.ResolveCommentParams{
		ID: commentUUID, ResolvedByType: pgtype.Text{String: "member", Valid: true}, ResolvedByID: userUUID,
	})
	if err != nil || resolved.Revision != 6 {
		t.Fatalf("duplicate resolve = (%+v, %v), want unchanged revision 6", resolved, err)
	}
	unresolved, err := testHandler.Queries.UnresolveComment(ctx, commentUUID)
	if err != nil || unresolved.Revision != 7 {
		t.Fatalf("unresolve comment = (%+v, %v), want revision 7", unresolved, err)
	}
	unresolved, err = testHandler.Queries.UnresolveComment(ctx, commentUUID)
	if err != nil || unresolved.Revision != 7 {
		t.Fatalf("duplicate unresolve = (%+v, %v), want unchanged revision 7", unresolved, err)
	}

	statusIssue, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issueUUID, Status: "in_progress", WorkspaceID: workspaceUUID,
	})
	if err != nil || statusIssue.Revision != 13 {
		t.Fatalf("update status = (%+v, %v), want revision 13", statusIssue, err)
	}
	statusIssue, err = testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issueUUID, Status: "in_progress", WorkspaceID: workspaceUUID,
	})
	if err != nil || statusIssue.Revision != 13 {
		t.Fatalf("duplicate status update = (%+v, %v), want unchanged revision 13", statusIssue, err)
	}
}
