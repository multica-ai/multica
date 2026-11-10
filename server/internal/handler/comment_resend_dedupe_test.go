package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// TestCreateComment_ConcurrentIdenticalResendsCollapse covers the overlapping
// case the guard exists for: one run firing the CLI twice at once. Both
// requests are in flight together, so a lookup taken before the write
// transaction lets each see "no existing comment" and both insert. The check
// has to run under the issue lock it shares with CreateComment.
func TestCreateComment_ConcurrentIdenticalResendsCollapse(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	const writers = 8
	for round := 0; round < 5; round++ {
		round := round
		t.Run(fmt.Sprintf("round%d", round), func(t *testing.T) {
			fx := newRunningSquadLeaderTaskFixture(t)
			verdict := fmt.Sprintf("【验收结果】并发重发 round %d", round)

			codes := make([]int, writers)
			ids := make([]string, writers)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < writers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					w := postAgentReply(t, fx, verdict)
					codes[i] = w.Code
					if w.Code == http.StatusCreated || w.Code == http.StatusOK {
						var body map[string]any
						_ = json.NewDecoder(w.Body).Decode(&body)
						ids[i], _ = body["id"].(string)
					}
				}(i)
			}
			close(start)
			wg.Wait()

			created := 0
			for i, code := range codes {
				switch code {
				case http.StatusCreated:
					created++
				case http.StatusOK:
				default:
					t.Fatalf("round %d writer %d: unexpected status %d", round, i, code)
				}
			}
			if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 1 || created != 1 {
				t.Fatalf("round %d: %d concurrent identical re-sends left %d comments (%d created), want exactly 1", round, writers, got, created)
			}
			for i := 1; i < writers; i++ {
				if ids[i] != ids[0] {
					t.Fatalf("round %d: writer %d got comment %s, writer 0 got %s; all must converge on one", round, i, ids[i], ids[0])
				}
			}
		})
	}
}
