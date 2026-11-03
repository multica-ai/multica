package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postAgentReply posts content as the fixture's agent run, replying under its
// trigger comment (a comment-triggered task may not create top-level comments).
func postAgentReply(t *testing.T, fx runningSquadLeaderTaskFixture, content string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content":   content,
		"parent_id": fx.TriggerCommentID,
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	return w
}

// commentIDFromResponse reads the comment id out of a CreateComment response.
func commentIDFromResponse(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("comment response carried no id: %v", body)
	}
	return id
}

// TestCreateComment_IdenticalResendFromSameRunIsCollapsed pins the re-send
// guard. On 2026-11-03 one run put byte-identical acceptance verdicts on five
// issues 1-2 seconds apart — it fired the CLI twice for one body — and every
// recipient was woken twice. A run has no reason to post the same text twice on
// one issue, so the second write returns the first comment instead of creating
// a second one.
func TestCreateComment_IdenticalResendFromSameRunIsCollapsed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)
	const verdict = "【验收结果】环境：dev；结论：部分通过"

	first := postAgentReply(t, fx, verdict)
	if first.Code != http.StatusCreated {
		t.Fatalf("first post: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	firstID := commentIDFromResponse(t, first)

	// The re-send. 200 rather than 201 says "already posted" without making the
	// caller distinguish it from having lost a race.
	second := postAgentReply(t, fx, verdict)
	if second.Code != http.StatusOK {
		t.Fatalf("re-send: expected 200 (collapsed), got %d: %s", second.Code, second.Body.String())
	}
	if secondID := commentIDFromResponse(t, second); secondID != firstID {
		t.Fatalf("re-send should return the first comment %s, got %s", firstID, secondID)
	}

	// The guard's whole point: one comment on the issue, so recipients are
	// notified once.
	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 1 {
		t.Fatalf("expected the re-send to be collapsed into 1 comment, got %d", got)
	}
}

// TestCreateComment_DifferentContentFromSameRunIsNotCollapsed is the guard's
// other half: a run legitimately posts several distinct comments in one turn
// (one per issue it forwards to, a result plus a hand-off). Only byte-identical
// bodies are re-sends.
func TestCreateComment_DifferentContentFromSameRunIsNotCollapsed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)

	if w := postAgentReply(t, fx, "验收结果：通过"); w.Code != http.StatusCreated {
		t.Fatalf("first post: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := postAgentReply(t, fx, "遗留事项需要你决定"); w.Code != http.StatusCreated {
		t.Fatalf("second distinct post: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 2 {
		t.Fatalf("expected 2 distinct comments to be kept, got %d", got)
	}
}
