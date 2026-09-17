package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func countReplyAdmissionCommentsForIssue(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	return count
}

// TestCreateComment_AgentOpinionReplyRequiresRequesterMention reproduces the
// live COM-86 / COM-105 failure at the server boundary: an agent can currently
// post a substantive answer to another agent's explicit opinion request with
// no same-comment mention, because the handler persists before it considers
// reply admission.
func TestCreateComment_AgentOpinionReplyRequiresRequesterMention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Opinion Requester", nil)
	responderID := createHandlerTestAgent(t, "Opinion Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission", "", "")
	parentID := dbfx.Comment(t, issueID, fmt.Sprintf(
		"Codex, what is your opinion? Do you read these options differently, and is my claim sound? [@Codex](mention://agent/%s)",
		responderID,
	), testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, responderID, issueID)

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "## Changes\n\nThe cost constraint is binding because the options differ materially on acquisition risk.",
		"parent_id": parentID,
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", responderID)
	r.Header.Set("X-Task-ID", taskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment missing requester mention: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForIssue(t, issueID, responderID); got != 0 {
		t.Fatalf("rejected substantive reply was persisted: got %d agent comments", got)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	message, _ := body["error"].(string)
	for _, want := range []string{"mention", requesterID} {
		if !strings.Contains(strings.ToLower(message), strings.ToLower(want)) {
			t.Fatalf("error message %q does not contain %q", message, want)
		}
	}
	if body["code"] != "agent_reply_admission_required" {
		t.Fatalf("error code = %v, want agent_reply_admission_required", body["code"])
	}
	if body["retryable"] != true {
		t.Fatalf("retryable = %v, want true", body["retryable"])
	}
	requiredMention, _ := body["required_mention"].(string)
	wantMention := "mention://agent/" + requesterID
	if requiredMention != wantMention {
		t.Fatalf("required mention = %q, want %q", requiredMention, wantMention)
	}
}

// TestCreateComment_AgentOpinionReplyWithRequesterMentionIsAccepted is the
// positive control for the same boundary. Suppression cannot remove the
// requester trigger that admission required.
func TestCreateComment_AgentOpinionReplyWithRequesterMentionIsAccepted(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Mentioned Requester", nil)
	responderID := createHandlerTestAgent(t, "Mentioned Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission positive", "", "")
	parentID := dbfx.Comment(t, issueID, "Please give your opinion on this review.", testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, responderID, issueID)

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": fmt.Sprintf(
			"The review is sound and the cost constraint is the binding variable. `server/internal/handler/comment.go:1894`\n\n[@Requester](mention://agent/%s)",
			requesterID,
		),
		"parent_id":          parentID,
		"suppress_agent_ids": []string{requesterID},
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", responderID)
	r.Header.Set("X-Task-ID", taskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment mentioned requester: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode mentioned requester response: %v", err)
	}
	outcome := findCommentOutcome(t, resp.TriggerOutcomes, requesterID)
	if outcome.Status == DispatchBlocked {
		t.Fatalf("required requester trigger was suppressed: %+v", outcome)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, requesterID); got != 1 {
		t.Fatalf("required requester queued tasks = %d, want 1", got)
	}
}

// TestCreateComment_AgentOpinionAcknowledgementRemainsExempt prevents the
// hard stop from turning a one-line acknowledgement into a paid agent loop.
func TestCreateComment_AgentOpinionAcknowledgementRemainsExempt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Acknowledgement Requester", nil)
	responderID := createHandlerTestAgent(t, "Acknowledgement Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission acknowledgement", "", "")
	parentID := dbfx.Comment(t, issueID, "What is your opinion on this review?", testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, responderID, issueID)

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "Acknowledged.",
		"parent_id": parentID,
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", responderID)
	r.Header.Set("X-Task-ID", taskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment acknowledgement: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateComment_AgentOpinionFencedSubstanceCannotUseAcknowledgementExemption(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Fenced Opinion Requester", nil)
	responderID := createHandlerTestAgent(t, "Fenced Opinion Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission fenced substance", "", "")
	parentID := dbfx.Comment(t, issueID, "What is your opinion on this review?", testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, responderID, issueID)

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":   "Noted.\n\n```md\nThe review is sound and the cost constraint is binding.\n```",
		"parent_id": parentID,
	})
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", responderID)
	r.Header.Set("X-Task-ID", taskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment fenced substance: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForIssue(t, issueID, responderID); got != 0 {
		t.Fatalf("rejected fenced substantive reply was persisted: got %d agent comments", got)
	}
}

// TestUpdateComment_AgentOpinionEditRequiresRequesterMention covers the
// second HTTP writer. Editing an already stored agent comment into a
// substantive opinion response must not bypass the same admission rule.
func TestUpdateComment_AgentOpinionEditRequiresRequesterMention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Edit Requester", nil)
	responderID := createHandlerTestAgent(t, "Edit Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission edit", "", "")
	parentID := dbfx.Comment(t, issueID, "Please review this and tell me your opinion.", testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	commentID := dbfx.Comment(t, issueID, "Acknowledged.", testutil.Cols{
		"author_type": "agent",
		"author_id":   responderID,
		"parent_id":   parentID,
	})
	taskID := createHandlerTestTaskForAgentOnIssue(t, responderID, issueID)

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content": "The review is sound, and the cost constraint is binding.",
	})
	r = withURLParam(r, "commentId", commentID)
	r.Header.Set("X-Agent-ID", responderID)
	r.Header.Set("X-Task-ID", taskID)

	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateComment missing requester mention: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var content string
	if err := testPool.QueryRow(context.Background(), `SELECT content FROM comment WHERE id = $1`, commentID).Scan(&content); err != nil {
		t.Fatalf("read comment after rejected edit: %v", err)
	}
	if content != "Acknowledged." {
		t.Fatalf("rejected edit changed content to %q", content)
	}
}

func TestUpdateComment_AdminCannotBypassAgentReplyAdmission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	requesterID := createHandlerTestAgent(t, "Admin Edit Requester", nil)
	responderID := createHandlerTestAgent(t, "Admin Edit Responder", nil)
	issueID := createCommentTriggerPreviewIssue(t, "server reply admission admin edit", "", "")
	parentID := dbfx.Comment(t, issueID, "Could you weigh in on this?", testutil.Cols{
		"author_type": "agent",
		"author_id":   requesterID,
	})
	commentID := dbfx.Comment(t, issueID, "Acknowledged.", testutil.Cols{
		"author_type": "agent",
		"author_id":   responderID,
		"parent_id":   parentID,
	})

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content": "The proposal is sound and the trade-off is material.",
	})
	r = withURLParam(r, "commentId", commentID)

	// newRequest authenticates as testUserID, the workspace owner. The stored
	// row is still agent-authored, so this exercises the admin-edit bypass.
	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("admin UpdateComment missing requester mention: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var content string
	if err := testPool.QueryRow(context.Background(), `SELECT content FROM comment WHERE id = $1`, commentID).Scan(&content); err != nil {
		t.Fatalf("read comment after rejected admin edit: %v", err)
	}
	if content != "Acknowledged." {
		t.Fatalf("rejected admin edit changed content to %q", content)
	}
}

func TestCreateComment_IdempotencyKeyReplaysWithoutDuplicateComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createCommentTriggerPreviewIssue(t, "comment idempotency", "", "")
	content := "A retry-safe comment."

	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
			"content": content,
		})
		r = withURLParam(r, "id", issueID)
		r.Header.Set("Idempotency-Key", "reply-admission-idempotency-1")
		testHandler.CreateComment(w, r)
		return w
	}

	first := post()
	if first.Code != http.StatusCreated {
		t.Fatalf("first comment: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	var completedAt *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT side_effects_completed_at
		FROM comment_idempotency
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, "reply-admission-idempotency-1").Scan(&completedAt); err != nil {
		t.Fatalf("read side-effect completion marker: %v", err)
	}
	if completedAt == nil {
		t.Fatal("successful idempotent create did not record side-effect completion")
	}
	second := post()
	if second.Code != http.StatusOK {
		t.Fatalf("replayed comment: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replayed header = %q, want true", second.Header().Get("Idempotency-Replayed"))
	}
	if got := countReplyAdmissionCommentsForIssue(t, issueID); got != 1 {
		t.Fatalf("idempotent replay created %d comments, want 1", got)
	}

	// Simulate a response lost after the comment commit but before the
	// completion marker. The same retry must run the recovery path and still
	// leave exactly one comment.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment_idempotency
		SET side_effects_completed_at = NULL,
		    side_effects_claimed_at = now() - interval '11 minutes'
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, "reply-admission-idempotency-1"); err != nil {
		t.Fatalf("clear side-effect completion marker: %v", err)
	}
	recovered := post()
	if recovered.Code != http.StatusOK {
		t.Fatalf("recovered replay: expected 200, got %d: %s", recovered.Code, recovered.Body.String())
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT side_effects_completed_at
		FROM comment_idempotency
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, testWorkspaceID, "reply-admission-idempotency-1").Scan(&completedAt); err != nil {
		t.Fatalf("read recovered completion marker: %v", err)
	}
	if completedAt == nil {
		t.Fatal("replay recovery left side-effect completion marker NULL")
	}
	if got := countReplyAdmissionCommentsForIssue(t, issueID); got != 1 {
		t.Fatalf("recovered replay created %d comments, want 1", got)
	}
}

func TestCreateComment_IdempotencyKeyRejectsChangedPayload(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createCommentTriggerPreviewIssue(t, "comment idempotency mismatch", "", "")
	post := func(content string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
			"content": content,
		})
		r = withURLParam(r, "id", issueID)
		r.Header.Set("Idempotency-Key", "reply-admission-idempotency-2")
		testHandler.CreateComment(w, r)
		return w
	}

	if first := post("original payload"); first.Code != http.StatusCreated {
		t.Fatalf("first comment: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	mismatch := post("changed payload")
	if mismatch.Code != http.StatusConflict {
		t.Fatalf("changed payload: expected 409, got %d: %s", mismatch.Code, mismatch.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(mismatch.Body).Decode(&body); err != nil {
		t.Fatalf("decode mismatch response: %v", err)
	}
	if body["code"] != "idempotency_key_reused" {
		t.Fatalf("mismatch code = %v, want idempotency_key_reused", body["code"])
	}
	if got := countReplyAdmissionCommentsForIssue(t, issueID); got != 1 {
		t.Fatalf("mismatched retry created %d comments, want 1", got)
	}
}

func TestCreateComment_IdempotencyKeyConcurrentRequestsCreateOneComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	issueID := createCommentTriggerPreviewIssue(t, "concurrent comment idempotency", "", "")
	const requestCount = 5
	results := make(chan int, requestCount)
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
				"content": "A concurrently retried comment.",
			})
			r = withURLParam(r, "id", issueID)
			r.Header.Set("Idempotency-Key", "reply-admission-concurrent-1")
			testHandler.CreateComment(w, r)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)

	created, replayed := 0, 0
	for status := range results {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			replayed++
		default:
			t.Fatalf("concurrent idempotent request returned unexpected status %d", status)
		}
	}
	if created != 1 || replayed != requestCount-1 {
		t.Fatalf("concurrent idempotent results: created=%d replayed=%d, want created=1 replayed=%d", created, replayed, requestCount-1)
	}
	if got := countReplyAdmissionCommentsForIssue(t, issueID); got != 1 {
		t.Fatalf("concurrent idempotent requests created %d comments, want 1", got)
	}
}
