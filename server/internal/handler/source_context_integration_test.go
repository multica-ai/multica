package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWriteSourceContextErrorHidesInternalDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).writeSourceContextError(recorder, errors.New("postgres password=do-not-leak"), service.SourceContextLimitUsage{})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "do-not-leak") {
		t.Fatalf("internal error leaked in response: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "failed to capture source context") {
		t.Fatalf("generic source-context error missing: %s", recorder.Body.String())
	}
}

func TestCommentSourceContextLifecycle(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	// Go runs different packages concurrently against the same integration
	// database. Serialize the two tests that deliberately invoke the global
	// source-context intent sweeper; otherwise the service-package fake store
	// can claim this test's rows (or vice versa) and invalidate both oracles.
	cleanupLockConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire source-context cleanup test lock connection: %v", err)
	}
	if _, err := cleanupLockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(0x53434f4e54455854)); err != nil {
		cleanupLockConn.Release()
		t.Fatalf("lock source-context cleanup tests: %v", err)
	}
	t.Cleanup(func() {
		_, _ = cleanupLockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(0x53434f4e54455854))
		cleanupLockConn.Release()
	})
	originalStorage := testHandler.Storage
	originalDownloadMode := testHandler.cfg.AttachmentDownloadMode
	originalBus := testHandler.Bus
	store := &mockStorage{}
	eventBus := events.New()
	testHandler.Storage = store
	testHandler.Bus = eventBus
	testHandler.cfg.AttachmentDownloadMode = string(attachmentDownloadModeProxy)
	t.Cleanup(func() {
		testHandler.Storage = originalStorage
		testHandler.Bus = originalBus
		testHandler.cfg.AttachmentDownloadMode = originalDownloadMode
	})
	var updatedEvents []events.Event
	eventBus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
		updatedEvents = append(updatedEvents, event)
	})
	number := int(time.Now().UnixNano()%100000) + 9_100_000
	var sourceIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, creator_type, creator_id, number)
		VALUES ($1, 'source issue', 'frozen issue body', 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, number).Scan(&sourceIssueID); err != nil {
		t.Fatalf("insert source issue: %v", err)
	}
	insertComment := func(content string, parent any) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, parent_id)
			VALUES ($1, $2, 'member', $3, $4, $5)
			RETURNING id
		`, sourceIssueID, testWorkspaceID, testUserID, content, parent).Scan(&id); err != nil {
			t.Fatalf("insert comment %q: %v", content, err)
		}
		return id
	}
	rootID := insertComment("root context", nil)
	earlierSiblingID := insertComment("earlier sibling context", rootID)
	earlierNestedID := insertComment("earlier nested context", earlierSiblingID)
	replyID := insertComment("reply context", rootID)
	selectedID := insertComment("selected context", rootID)
	laterSiblingID := insertComment("later sibling excluded", rootID)
	laterDescendantID := insertComment("later descendant excluded", selectedID)
	insertAttachment := func(ownerCommentID any, filename, key, body string) string {
		t.Helper()
		store.put(key, []byte(body))
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO attachment (
				workspace_id, issue_id, comment_id, uploader_type, uploader_id,
				filename, url, content_type, size_bytes
			) VALUES ($1, $2, $3, 'member', $4, $5, $6, 'text/plain', $7)
			RETURNING id
		`, testWorkspaceID, sourceIssueID, ownerCommentID, testUserID, filename, store.ObjectURL(key), len(body)).Scan(&id); err != nil {
			t.Fatalf("insert attachment %q: %v", filename, err)
		}
		return id
	}
	issueAttachmentID := insertAttachment(nil, "issue.txt", "source/issue.txt", "issue attachment")
	selectedAttachmentID := insertAttachment(selectedID, "selected.txt", "source/selected.txt", "selected attachment")
	earlierSiblingAttachmentID := insertAttachment(earlierSiblingID, "earlier-sibling.txt", "source/earlier-sibling.txt", "earlier sibling attachment")
	laterSiblingAttachmentID := insertAttachment(laterSiblingID, "later-sibling.txt", "source/later-sibling.txt", "later sibling attachment")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = $1 WHERE id = $2`,
		"frozen issue body\n\n!file[issue.txt](/api/attachments/"+issueAttachmentID+"/download)", sourceIssueID); err != nil {
		t.Fatalf("embed issue attachment in source description: %v", err)
	}
	var targetIssueID string
	t.Cleanup(func() {
		if targetIssueID != "" {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id IN (SELECT id FROM issue_source_context WHERE issue_id = $1)`, targetIssueID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE issue_id = $1`, targetIssueID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, targetIssueID)
		}
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, sourceIssueID)
	})

	previewRecorder := httptest.NewRecorder()
	previewRequest := withURLParam(newRequest(http.MethodGet, "/api/comments/"+selectedID+"/sub-issue-preview", nil), "commentId", selectedID)
	testHandler.PreviewCommentSubIssue(previewRecorder, previewRequest)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", previewRecorder.Code, previewRecorder.Body.String())
	}
	var preview sourceContextPreviewResponse
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	wantThreadHistory := []string{rootID, earlierSiblingID, earlierNestedID, replyID, selectedID}
	if len(preview.CommentThread) != len(wantThreadHistory) {
		t.Fatalf("preview thread history length = %d, want %d: %#v", len(preview.CommentThread), len(wantThreadHistory), preview.CommentThread)
	}
	for i, comment := range preview.CommentThread {
		if comment.ID != wantThreadHistory[i] {
			t.Fatalf("preview thread history[%d] = %s, want %s", i, comment.ID, wantThreadHistory[i])
		}
		if comment.Attachments == nil {
			t.Fatalf("preview comment %s encoded attachments as null, want an array", comment.ID)
		}
		if comment.ID == laterSiblingID || comment.ID == laterDescendantID {
			t.Fatalf("preview included comment after the selected boundary: %s", comment.ID)
		}
	}
	if len(preview.SourceIssue.Attachments) != 1 || preview.SourceIssue.Attachments[0].ID != issueAttachmentID {
		t.Fatalf("preview issue attachments = %#v, want only %s", preview.SourceIssue.Attachments, issueAttachmentID)
	}
	selectedPreview := preview.CommentThread[len(preview.CommentThread)-1]
	if len(selectedPreview.Attachments) != 1 || selectedPreview.Attachments[0].ID != selectedAttachmentID {
		t.Fatalf("preview selected attachments = %#v, want only %s", selectedPreview.Attachments, selectedAttachmentID)
	}
	var foundEarlierSiblingAttachment bool
	for _, comment := range preview.CommentThread {
		for _, attachment := range comment.Attachments {
			if attachment.ID == earlierSiblingAttachmentID {
				foundEarlierSiblingAttachment = true
			}
			if attachment.ID == laterSiblingAttachmentID {
				t.Fatal("preview included attachment from a comment after the selected boundary")
			}
		}
	}
	if !foundEarlierSiblingAttachment {
		t.Fatal("preview omitted attachment from earlier sibling in the selected thread")
	}

	// Revision-only churn is metadata, not canonical source content.
	if _, err := testPool.Exec(ctx, `UPDATE comment SET revision = revision + 1 WHERE id = $1`, selectedID); err != nil {
		t.Fatalf("bump revision: %v", err)
	}
	rebuilt, err := service.BuildSourceContext(ctx, testHandler.Queries, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(selectedID))
	if err != nil {
		t.Fatalf("rebuild after revision-only change: %v", err)
	}
	if rebuilt.Token != preview.CaptureToken {
		t.Fatalf("revision-only change altered token: %q != %q", rebuilt.Token, preview.CaptureToken)
	}

	// Canonical content changes reject the stale preview without creating a
	// target. Restoring the exact content restores the original digest.
	if _, err := testPool.Exec(ctx, `UPDATE comment SET content = 'changed before submit', revision = revision + 1, updated_at = now() WHERE id = $1`, earlierSiblingID); err != nil {
		t.Fatalf("edit earlier thread comment before submit: %v", err)
	}
	staleRecorder := httptest.NewRecorder()
	staleRequest := withURLParam(newRequest(http.MethodPost, "/api/comments/"+selectedID+"/sub-issues", map[string]any{
		"mode": "manual", "capture_token": preview.CaptureToken,
		"issue": map[string]any{"title": "must not be created", "status": "todo", "priority": "none"},
	}), "commentId", selectedID)
	testHandler.CreateCommentSubIssue(staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusConflict {
		t.Fatalf("stale source create = %d: %s", staleRecorder.Code, staleRecorder.Body.String())
	}
	var staleBody map[string]any
	if err := json.Unmarshal(staleRecorder.Body.Bytes(), &staleBody); err != nil || staleBody["code"] != "source_context_changed" {
		t.Fatalf("stale source body = %#v, err=%v", staleBody, err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE comment SET content = 'earlier sibling context', revision = revision + 1, updated_at = now() WHERE id = $1`, earlierSiblingID); err != nil {
		t.Fatalf("restore earlier thread comment: %v", err)
	}

	// A damaged cross-issue parent chain is rejected instead of truncated.
	var foreignIssueID, foreignRootID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		VALUES ($1, 'foreign chain issue', 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, number+1).Scan(&foreignIssueID); err != nil {
		t.Fatalf("insert foreign issue: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'foreign root') RETURNING id
	`, foreignIssueID, testWorkspaceID, testUserID).Scan(&foreignRootID); err != nil {
		t.Fatalf("insert foreign root: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE comment SET parent_id = $1 WHERE id = $2`, foreignRootID, selectedID); err != nil {
		t.Fatalf("damage parent chain: %v", err)
	}
	if _, err := service.BuildSourceContext(ctx, testHandler.Queries, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(selectedID)); !errors.Is(err, service.ErrSourceContextInvalid) {
		t.Fatalf("damaged chain error = %v, want ErrSourceContextInvalid", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE comment SET parent_id = $1 WHERE id = $2`, rootID, selectedID); err != nil {
		t.Fatalf("restore parent chain: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, foreignIssueID); err != nil {
		t.Fatalf("delete foreign issue: %v", err)
	}

	createRecorder := httptest.NewRecorder()
	createRequest := withURLParam(newRequest(http.MethodPost, "/api/comments/"+selectedID+"/sub-issues", map[string]any{
		"mode":          "manual",
		"capture_token": preview.CaptureToken,
		"issue": map[string]any{
			"title": "new independent task", "description": "new instructions", "status": "todo", "priority": "none", "stage": 2,
		},
	}), "commentId", selectedID)
	testHandler.CreateCommentSubIssue(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("manual source create = %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created IssueResponse
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	targetIssueID = created.ID
	if created.ParentIssueID == nil || *created.ParentIssueID != sourceIssueID || created.Stage == nil || *created.Stage != 2 {
		t.Fatalf("created relation = parent %#v stage %#v", created.ParentIssueID, created.Stage)
	}
	var contextID string
	var storedSnapshot []byte
	if err := testPool.QueryRow(ctx, `SELECT id, snapshot FROM issue_source_context WHERE issue_id = $1 AND state = 'attached'`, targetIssueID).Scan(&contextID, &storedSnapshot); err != nil {
		t.Fatalf("load attached context: %v", err)
	}
	var snapshot service.SourceContextSnapshot
	if err := json.Unmarshal(storedSnapshot, &snapshot); err != nil {
		t.Fatalf("decode stored snapshot: %v", err)
	}
	if snapshot.Version != service.SourceContextVersion || snapshot.CapturedByUserID != testUserID || snapshot.CapturedAt == "" {
		t.Fatalf("stored capture metadata = version %d user %q at %q", snapshot.Version, snapshot.CapturedByUserID, snapshot.CapturedAt)
	}
	selectedSnapshot := snapshot.CommentThread[len(snapshot.CommentThread)-1]
	if len(snapshot.SourceIssue.Attachments) != 1 || len(selectedSnapshot.Attachments) != 1 {
		t.Fatalf("stored clone ownership = issue %#v selected %#v", snapshot.SourceIssue.Attachments, selectedSnapshot.Attachments)
	}
	cloneID := selectedSnapshot.Attachments[0].ID
	if cloneID == selectedAttachmentID || selectedSnapshot.Attachments[0].SourceAttachmentID != selectedAttachmentID {
		t.Fatalf("stored clone ids = %#v", selectedSnapshot.Attachments[0])
	}
	cloneKey := "workspaces/" + testWorkspaceID + "/source-context/" + cloneID + ".txt"
	deleteCloneRecorder := httptest.NewRecorder()
	deleteCloneRequest := withURLParam(newRequest(http.MethodDelete, "/api/attachments/"+cloneID, nil), "id", cloneID)
	testHandler.DeleteAttachment(deleteCloneRecorder, deleteCloneRequest)
	if deleteCloneRecorder.Code != http.StatusNotFound {
		t.Fatalf("delete immutable source-context clone = %d: %s", deleteCloneRecorder.Code, deleteCloneRecorder.Body.String())
	}
	if _, err := store.GetReader(ctx, cloneKey); err != nil {
		t.Fatalf("immutable source-context clone missing after direct delete: %v", err)
	}

	// Editing live source changes detail state without mutating the stored bytes.
	if _, err := testPool.Exec(ctx, `UPDATE comment SET content = 'edited after capture', revision = revision + 1, updated_at = now() WHERE id = $1`, rootID); err != nil {
		t.Fatalf("edit source root: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(targetIssueID))
	if err != nil {
		t.Fatalf("load target issue: %v", err)
	}
	detail, err := testHandler.issueSourceContextDetail(ctx, issue)
	if err != nil {
		t.Fatalf("load changed source detail: %v", err)
	}
	if detail.DisplayState != "changed" || detail.CommentThreadState != "changed" || !detail.CanOpenCurrentSource || detail.Snapshot.CommentThread[0].Content != "root context" {
		t.Fatalf("changed detail = display %q path %q snapshot root %q", detail.DisplayState, detail.CommentThreadState, detail.Snapshot.CommentThread[0].Content)
	}
	if len(detail.ChangeReasons) != 1 || detail.ChangeReasons[0] != sourceContextChangeCommentThread {
		t.Fatalf("changed detail reasons = %v, want comment thread history", detail.ChangeReasons)
	}
	if !reflect.DeepEqual(detail.ChangeDetails.ChangedCommentIDs, []string{rootID}) {
		t.Fatalf("changed comment ids = %v, want root comment", detail.ChangeDetails.ChangedCommentIDs)
	}
	selectedDetailSnapshot := detail.Snapshot.CommentThread[len(detail.Snapshot.CommentThread)-1]
	if got := selectedDetailSnapshot.Attachments[0].ID; got != cloneID {
		t.Fatalf("detail snapshot attachment id = %q, want clone id %q", got, cloneID)
	}
	if selectedDetailSnapshot.Revision == 0 || selectedDetailSnapshot.UpdatedAt == "" {
		t.Fatalf("detail snapshot lost capture metadata: %#v", selectedDetailSnapshot)
	}
	var storedSnapshotAfter []byte
	if err := testPool.QueryRow(ctx, `SELECT snapshot FROM issue_source_context WHERE issue_id = $1`, targetIssueID).Scan(&storedSnapshotAfter); err != nil {
		t.Fatalf("reload snapshot: %v", err)
	}
	if string(storedSnapshotAfter) != string(storedSnapshot) {
		t.Fatal("stored snapshot changed after live source edit")
	}

	// Thread drift and anchor-comment existence are independent. Re-rooting an
	// earlier comment keeps the anchor openable while changing the thread.
	if _, err := testPool.Exec(ctx, `UPDATE comment SET parent_id = NULL WHERE id = $1`, replyID); err != nil {
		t.Fatalf("re-root source reply: %v", err)
	}
	changedPathDetail, err := testHandler.issueSourceContextDetail(ctx, issue)
	if err != nil {
		t.Fatalf("load changed-path detail: %v", err)
	}
	if changedPathDetail.CommentThreadState != "changed" || !changedPathDetail.CanOpenCurrentSource || changedPathDetail.CurrentSource == nil {
		t.Fatalf("changed-thread detail = thread %q open=%v current=%#v", changedPathDetail.CommentThreadState, changedPathDetail.CanOpenCurrentSource, changedPathDetail.CurrentSource)
	}
	foundThreadHistoryReason := false
	for _, reason := range changedPathDetail.ChangeReasons {
		if reason == sourceContextChangeCommentThread {
			foundThreadHistoryReason = true
		}
	}
	if !foundThreadHistoryReason {
		t.Fatalf("changed-thread reasons = %v, want comment thread", changedPathDetail.ChangeReasons)
	}
	if _, err := testPool.Exec(ctx, `UPDATE comment SET parent_id = $1 WHERE id = $2`, rootID, replyID); err != nil {
		t.Fatalf("restore source reply parent: %v", err)
	}

	// Captured attachments are independent clones. Removing the attachment node
	// from the live description reports the user-visible reference change even
	// though the issue-owned attachment row remains. The cloned bytes remain
	// downloadable from the captured context.
	issueCloneID := snapshot.SourceIssue.Attachments[0].ID
	if _, err := testPool.Exec(ctx, `UPDATE issue SET description = 'frozen issue body', revision = revision + 1, updated_at = now() WHERE id = $1`, sourceIssueID); err != nil {
		t.Fatalf("remove live issue attachment reference: %v", err)
	}
	attachmentDetail, err := testHandler.issueSourceContextDetail(ctx, issue)
	if err != nil {
		t.Fatalf("load attachment-changed detail: %v", err)
	}
	if attachmentDetail.SourceIssueState != "changed" || !attachmentDetail.CanOpenCurrentSource {
		t.Fatalf("attachment detail = issue %q open=%v", attachmentDetail.SourceIssueState, attachmentDetail.CanOpenCurrentSource)
	}
	foundAttachmentReason := false
	for _, reason := range attachmentDetail.ChangeReasons {
		if reason == sourceContextChangeIssueDescriptionAttachments {
			foundAttachmentReason = true
		}
	}
	if !foundAttachmentReason {
		t.Fatalf("attachment reasons = %v, want issue description attachments", attachmentDetail.ChangeReasons)
	}
	if !reflect.DeepEqual(attachmentDetail.ChangeDetails.DescriptionAttachmentChanges, []sourceContextDescriptionAttachmentChange{{
		Kind: "removed", AttachmentID: issueAttachmentID, Filename: "issue.txt",
	}}) {
		t.Fatalf("attachment changes = %#v, want removed issue.txt from description", attachmentDetail.ChangeDetails.DescriptionAttachmentChanges)
	}
	issueDownloadRecorder := httptest.NewRecorder()
	issueDownloadRequest := withURLParam(newRequest(http.MethodGet, "/api/attachments/"+issueCloneID+"/download", nil), "id", issueCloneID)
	testHandler.DownloadAttachment(issueDownloadRecorder, issueDownloadRequest)
	if issueDownloadRecorder.Code != http.StatusOK || issueDownloadRecorder.Body.String() != "issue attachment" {
		t.Fatalf("snapshot issue clone download after live deletion = %d %q", issueDownloadRecorder.Code, issueDownloadRecorder.Body.String())
	}

	// Once the anchor comment is deleted, issue drift is still reported
	// independently. The viewer must not claim the source issue is unchanged
	// merely because it can no longer rebuild the comment thread history.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET title = 'source issue changed after capture', revision = revision + 1, updated_at = now() WHERE id = $1`, sourceIssueID); err != nil {
		t.Fatalf("edit source issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, selectedID); err != nil {
		t.Fatalf("delete anchor comment: %v", err)
	}
	deletedPathDetail, err := testHandler.issueSourceContextDetail(ctx, issue)
	if err != nil {
		t.Fatalf("load deleted-path detail: %v", err)
	}
	if deletedPathDetail.CommentThreadState != "changed" || deletedPathDetail.AnchorCommentState != "deleted" || deletedPathDetail.SourceIssueState != "changed" || deletedPathDetail.CanOpenCurrentSource {
		t.Fatalf("deleted-anchor detail = thread %q anchor %q issue %q open=%v", deletedPathDetail.CommentThreadState, deletedPathDetail.AnchorCommentState, deletedPathDetail.SourceIssueState, deletedPathDetail.CanOpenCurrentSource)
	}
	if !reflect.DeepEqual(deletedPathDetail.ChangeDetails.RemovedCommentIDs, []string{selectedID}) {
		t.Fatalf("deleted-anchor comment ids = %v, want anchor comment", deletedPathDetail.ChangeDetails.RemovedCommentIDs)
	}
	if !reflect.DeepEqual(deletedPathDetail.ChangeDetails.ChangedCommentIDs, []string{rootID}) {
		t.Fatalf("deleted-anchor changed comment ids = %v, want edited root comment", deletedPathDetail.ChangeDetails.ChangedCommentIDs)
	}

	// Deleting the source explicitly detaches the target, clears stage, bumps
	// revision, and leaves its immutable context readable.
	beforeRevision := issue.Revision
	deleteRecorder := httptest.NewRecorder()
	testHandler.DeleteIssue(deleteRecorder, withURLParam(newRequest(http.MethodDelete, "/api/issues/"+sourceIssueID, nil), "id", sourceIssueID))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete source issue = %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	sourceIssueID = ""
	detached, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(targetIssueID))
	if err != nil {
		t.Fatalf("load detached target: %v", err)
	}
	if detached.ParentIssueID.Valid || detached.Stage.Valid || detached.Revision != beforeRevision+1 {
		t.Fatalf("detached target = parent %v stage %v revision %d, want null/null/%d", detached.ParentIssueID.Valid, detached.Stage.Valid, detached.Revision, beforeRevision+1)
	}
	if len(updatedEvents) != 1 {
		t.Fatalf("detached child update events = %d, want 1", len(updatedEvents))
	}
	updatePayload, ok := updatedEvents[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("detached child update payload = %#v", updatedEvents[0].Payload)
	}
	updatedIssue, ok := updatePayload["issue"].(IssueResponse)
	if !ok || updatedIssue.ID != targetIssueID || updatedIssue.ParentIssueID != nil || updatedIssue.Stage != nil {
		t.Fatalf("detached child update issue = %#v", updatePayload["issue"])
	}
	deletedDetail, err := testHandler.issueSourceContextDetail(ctx, detached)
	if err != nil {
		t.Fatalf("load deleted-source detail: %v", err)
	}
	if deletedDetail.SourceIssueState != "deleted" || deletedDetail.CanOpenCurrentSource {
		t.Fatalf("deleted-source detail = state %q open=%v", deletedDetail.SourceIssueState, deletedDetail.CanOpenCurrentSource)
	}
	downloadRecorder := httptest.NewRecorder()
	downloadRequest := withURLParam(newRequest(http.MethodGet, "/api/attachments/"+cloneID+"/download", nil), "id", cloneID)
	testHandler.DownloadAttachment(downloadRecorder, downloadRequest)
	if downloadRecorder.Code != http.StatusOK || downloadRecorder.Body.String() != "selected attachment" {
		t.Fatalf("snapshot clone download after source deletion = %d %q", downloadRecorder.Code, downloadRecorder.Body.String())
	}

	// Deleting the target removes DB ownership atomically and leaves durable
	// intents for the legacy no-result bulk object deletion path. The sweeper
	// then reclaims exactly those now-unreferenced clone objects.
	targetDeleteRecorder := httptest.NewRecorder()
	testHandler.DeleteIssue(targetDeleteRecorder, withURLParam(newRequest(http.MethodDelete, "/api/issues/"+targetIssueID, nil), "id", targetIssueID))
	if targetDeleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete target issue = %d: %s", targetDeleteRecorder.Code, targetDeleteRecorder.Body.String())
	}
	targetIssueID = ""
	var contextExists, cloneExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_source_context WHERE id = $1)`, contextID).Scan(&contextExists); err != nil {
		t.Fatalf("check deleted context: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachment WHERE source_context_id = $1)`, contextID).Scan(&cloneExists); err != nil {
		t.Fatalf("check deleted clones: %v", err)
	}
	var intentCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_source_context_object_intent WHERE source_context_id = $1`, contextID).Scan(&intentCount); err != nil {
		t.Fatalf("count target delete intents: %v", err)
	}
	if contextExists || cloneExists || intentCount != 3 {
		t.Fatalf("target cleanup = context=%v clones=%v intents=%d, want false/false/3", contextExists, cloneExists, intentCount)
	}
	// Keep the due intents and the sweep behind a table lock. A developer
	// server may be running against this worktree database and its background
	// sweeper does not participate in the test-only advisory lock above.
	cleanupTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolated target intent cleanup: %v", err)
	}
	defer cleanupTx.Rollback(ctx)
	if _, err := cleanupTx.Exec(ctx, `LOCK TABLE issue_source_context_object_intent IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock target intent table: %v", err)
	}
	if _, err := cleanupTx.Exec(ctx, `
		UPDATE issue_source_context_object_intent
		SET created_at = now() - interval '2 hours', next_attempt_at = '-infinity'::timestamptz
		WHERE source_context_id = $1
	`, contextID); err != nil {
		t.Fatalf("age target delete intents: %v", err)
	}
	originalObjectStore := testHandler.TaskService.SourceContextStorage
	originalQueries := testHandler.TaskService.Queries
	testHandler.TaskService.SourceContextStorage = store
	testHandler.TaskService.Queries = originalQueries.WithTx(cleanupTx)
	cleaned, err := testHandler.TaskService.CleanupSourceContextObjectIntents(ctx, 10)
	testHandler.TaskService.SourceContextStorage = originalObjectStore
	testHandler.TaskService.Queries = originalQueries
	if err != nil || cleaned < 3 {
		t.Fatalf("cleanup target delete intents = %d, err=%v", cleaned, err)
	}
	if err := cleanupTx.Commit(ctx); err != nil {
		t.Fatalf("commit isolated target intent cleanup: %v", err)
	}
	if _, err := store.GetReader(ctx, cloneKey); err == nil {
		t.Fatal("target clone object still readable after durable cleanup")
	}
}

func TestBatchDeleteParentAndChildPublishesOnlySurvivingDetach(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	number := int(time.Now().UnixNano()%100000) + 9_200_000
	insertIssue := func(title string, parent any, stage any) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_type, creator_id, number, parent_issue_id, stage)
			VALUES ($1, $2, 'member', $3, $4, $5, $6) RETURNING id
		`, testWorkspaceID, title, testUserID, number, parent, stage).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", title, err)
		}
		number++
		return id
	}
	parentID := insertIssue("batch detach parent", nil, nil)
	childID := insertIssue("batch deleted child", parentID, 1)
	grandchildID := insertIssue("batch surviving grandchild", childID, 2)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = ANY($1::uuid[])`, []string{grandchildID, childID, parentID})
	})

	originalBus := testHandler.Bus
	eventBus := events.New()
	testHandler.Bus = eventBus
	t.Cleanup(func() { testHandler.Bus = originalBus })
	var updatedIDs []string
	eventBus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		issue, _ := payload["issue"].(IssueResponse)
		updatedIDs = append(updatedIDs, issue.ID)
	})

	recorder := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(recorder, newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{
		"issue_ids": []string{parentID, childID, parentID},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("batch delete = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]int
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response["deleted"] != 2 {
		t.Fatalf("batch delete response = %#v, err=%v", response, err)
	}
	grandchild, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(grandchildID))
	if err != nil {
		t.Fatalf("load surviving grandchild: %v", err)
	}
	if grandchild.ParentIssueID.Valid || grandchild.Stage.Valid {
		t.Fatalf("surviving grandchild relation = parent %v stage %v", grandchild.ParentIssueID.Valid, grandchild.Stage.Valid)
	}
	if len(updatedIDs) != 1 || updatedIDs[0] != grandchildID {
		t.Fatalf("batch detach update ids = %#v, want only %s", updatedIDs, grandchildID)
	}
}
