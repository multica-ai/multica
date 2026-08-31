package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func issueCommentFollowUpRequest(issueID, commentID, actionID string) *http.Request {
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments/"+commentID+"/follow-ups/"+actionID+"/run", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("commentId", commentID)
	rctx.URLParams.Add("actionId", actionID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRunIssueCommentFollowUpConcurrentClicksCreateOneReply(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Comment Follow-up Agent", []byte("null"))
	issueID := dbfx.Issue(t, "Run a comment follow-up")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"issue_id":     issueID,
		"status":       "completed",
		"completed_at": testutil.Raw("now()"),
	})
	commentID := dbfx.Comment(t, issueID, "I finished the first pass.", testutil.Cols{
		"author_type":          "agent",
		"author_id":            agentID,
		"source_task_id":       taskID,
		"suggested_follow_ups": testutil.Raw(`'[{"id":"continue-1","label":"Continue","prompt":"Continue with the focused revision.","primary":true},{"id":"review-1","label":"Review","prompt":"Review the current result."}]'::jsonb`),
	})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	recorders := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, recorder := range recorders {
		wg.Add(1)
		go func(rr *httptest.ResponseRecorder) {
			defer wg.Done()
			<-start
			testHandler.RunIssueCommentFollowUp(rr, issueCommentFollowUpRequest(issueID, commentID, "continue-1"))
		}(recorder)
	}
	close(start)
	wg.Wait()

	statuses := []int{recorders[0].Code, recorders[1].Code}
	sort.Ints(statuses)
	if statuses[0] != http.StatusCreated || statuses[1] != http.StatusConflict {
		t.Fatalf("concurrent run statuses = %v, want [201 409]: first=%s second=%s",
			statuses, recorders[0].Body.String(), recorders[1].Body.String())
	}

	var replyCount int
	var replyContent string
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), min(content)
		FROM comment
		WHERE issue_id = $1 AND parent_id = $2 AND author_type = 'member'
	`, issueID, commentID).Scan(&replyCount, &replyContent); err != nil {
		t.Fatalf("read follow-up reply: %v", err)
	}
	if replyCount != 1 || !strings.Contains(replyContent, "Continue with the focused revision.") ||
		!strings.Contains(replyContent, "mention://agent/"+agentID) {
		t.Fatalf("unexpected follow-up reply count=%d content=%q", replyCount, replyContent)
	}

	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND parent_id = $2 AND author_type = 'member'
	`, issueID, commentID).Scan(&replyCount); err != nil {
		t.Fatalf("re-read follow-up replies: %v", err)
	}
	if replyCount != 1 {
		t.Fatalf("reusing a follow-up created %d replies, want exactly 1", replyCount)
	}
}
