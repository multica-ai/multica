package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateComment_TriggeredTaskRejectsTopLevelComment exercises the full
// CreateComment handler path (not just taskCoversReplyParent) for the trap
// reported in MUL-4417 / GH #5266: a comment-triggered task that posts a
// parentless, top-level comment on its own issue is rejected with a 409 whose
// message names the trigger comment and states that top-level comments are not
// allowed. Pinning the message here keeps it from silently drifting away from
// the behavior the CLI help now documents.
func TestCreateComment_TriggeredTaskRejectsTopLevelComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content": "dispatching a squad from this task",
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment top-level: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 0 {
		t.Fatalf("expected rejected top-level comment not to be stored, got %d", got)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	msg, _ := body["error"].(string)
	// Pin the three semantic pieces without locking the exact wording: why it
	// was rejected, the comment to reply under, and the actionable fix. The last
	// one guards against the guidance being dropped in a future edit.
	for _, want := range []string{
		"top-level comments",   // reason
		fx.TriggerCommentID,    // the comment to reply under
		"parent_id (--parent)", // actionable fix
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("409 message should contain %q, got %q", want, msg)
		}
	}
}

// TestCreateComment_TriggeredTaskAllowsReplyUnderTrigger is the positive half:
// the same task replying under its trigger comment succeeds, proving the guard
// rejects only the top-level case and does not lock the whole issue for
// comments (MUL-4417 / GH #5266).
func TestCreateComment_TriggeredTaskAllowsReplyUnderTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content":   "replying under the trigger comment",
		"parent_id": fx.TriggerCommentID,
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment reply-under-trigger: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
