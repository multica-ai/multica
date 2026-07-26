package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func enableLifeOSLocalModeForTest(t *testing.T) {
	t.Helper()
	previous := testHandler.cfg.LocalMode
	testHandler.cfg.LocalMode = true
	t.Cleanup(func() {
		testHandler.cfg.LocalMode = previous
	})
}

func performLifeOSCommentReconcileForTest(
	t *testing.T,
	body map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.ReconcileLifeOSComments(
		recorder,
		newRequest(http.MethodPost, "/api/lifeos/comments/reconcile", body),
	)
	return recorder
}

func TestReconcileLifeOSCommentsUnavailableOutsideLocalMode(t *testing.T) {
	handler := &Handler{cfg: Config{LocalMode: false}}
	recorder := httptest.NewRecorder()
	handler.ReconcileLifeOSComments(
		recorder,
		newRequest(http.MethodPost, "/api/lifeos/comments/reconcile", nil),
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestReconcileLifeOSCommentsRejectsAgentSession(t *testing.T) {
	handler := &Handler{cfg: Config{LocalMode: true}}
	request := newRequest(http.MethodPost, "/api/lifeos/comments/reconcile", nil)
	request.Header.Set("X-Actor-Source", auth.LocalAgentActorSource)
	request.Header.Set("X-Agent-ID", "11111111-1111-1111-1111-111111111111")
	recorder := httptest.NewRecorder()
	handler.ReconcileLifeOSComments(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestLifeOSChairmanCommentAlwaysRoutesToCEO(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	enableLifeOSLocalModeForTest(t)

	ceoID := createHandlerTestAgent(t, localLifeOSCEOAgentName, nil)
	otherAgentID := createHandlerTestAgent(t, "LifeOS routing decoy", nil)
	issueID := createCommentTriggerPreviewIssue(
		t,
		"LifeOS chairman comment on member-assigned blocked issue",
		"member",
		testUserID,
	)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue SET status = 'blocked' WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue blocked: %v", err)
	}

	content := fmt.Sprintf(
		"这是董事长的补充，即使写了 [@Other](mention://agent/%s) 也由 AI 星耀统一受理",
		otherAgentID,
	)
	preview := previewCommentTriggersForTest(t, issueID, CommentTriggerPreviewRequest{
		Content: content,
	})
	requirePreviewAgents(t, preview, ceoID)
	if preview.Agents[0].Source != string(commentTriggerSourceLifeOSChairman) {
		t.Fatalf(
			"preview source = %q, want %q",
			preview.Agents[0].Source,
			commentTriggerSourceLifeOSChairman,
		)
	}

	postCommentForTriggerPreviewTest(t, issueID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{ceoID},
	})
	if got := countQueuedCommentTriggerTasks(t, issueID, ceoID); got != 1 {
		t.Fatalf("queued AI 星耀 tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, otherAgentID); got != 0 {
		t.Fatalf("queued explicitly mentioned decoy tasks = %d, want 0", got)
	}
}

func TestLifeOSChairmanNoteDoesNotRouteToCEO(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	enableLifeOSLocalModeForTest(t)

	ceoID := createHandlerTestAgent(t, localLifeOSCEOAgentName, nil)
	issueID := createCommentTriggerPreviewIssue(t, "LifeOS note opt-out", "member", testUserID)

	for _, content := range []string{
		"/note 这条只做记录",
		"仅记录，无需回复\n这条也是背景材料",
	} {
		preview := previewCommentTriggersForTest(t, issueID, CommentTriggerPreviewRequest{
			Content: content,
		})
		requirePreviewAgents(t, preview)
		postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": content})
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, ceoID); got != 0 {
		t.Fatalf("queued AI 星耀 tasks for note comments = %d, want 0", got)
	}
}

func TestReconcileLifeOSCommentsQueuesOnlyUnhandledChairmanComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	enableLifeOSLocalModeForTest(t)

	ceoID := createHandlerTestAgent(t, localLifeOSCEOAgentName, nil)
	issueID := createCommentTriggerPreviewIssue(t, "LifeOS missed comment recovery", "member", testUserID)
	commentID := insertMemberRootCommentForTriggerPreviewTest(t, issueID, "请继续处理这个被遗漏的评论")
	_ = insertMemberRootCommentForTriggerPreviewTest(t, issueID, "仅记录，无需回复")

	recorder := performLifeOSCommentReconcileForTest(t, map[string]any{
		"lookback_hours": 72,
		"limit":          100,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("ReconcileLifeOSComments status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var firstResponse LifeOSCommentReconcileResponse
	if err := json.NewDecoder(recorder.Body).Decode(&firstResponse); err != nil {
		t.Fatalf("decode first reconciliation response: %v", err)
	}
	if firstResponse.Scanned != 1 || firstResponse.Queued != 1 || firstResponse.Skipped != 0 {
		t.Fatalf("first reconciliation response = %+v, want one queued comment", firstResponse)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, ceoID); got != 1 {
		t.Fatalf("queued recovered AI 星耀 tasks = %d, want 1", got)
	}

	var triggerCommentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT trigger_comment_id::text
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issueID, ceoID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("read recovered trigger comment: %v", err)
	}
	if triggerCommentID != commentID {
		t.Fatalf("trigger comment = %s, want %s", triggerCommentID, commentID)
	}

	recorder = performLifeOSCommentReconcileForTest(t, map[string]any{
		"lookback_hours": 72,
		"limit":          100,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("second ReconcileLifeOSComments status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var secondResponse LifeOSCommentReconcileResponse
	if err := json.NewDecoder(recorder.Body).Decode(&secondResponse); err != nil {
		t.Fatalf("decode second reconciliation response: %v", err)
	}
	if secondResponse.Scanned != 0 {
		t.Fatalf("second reconciliation response = %+v, want no remaining comments", secondResponse)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, ceoID); got != 1 {
		t.Fatalf("queued recovered tasks after idempotent retry = %d, want 1", got)
	}
}
