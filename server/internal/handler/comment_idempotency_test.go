package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func createCommentWithKeyForTest(t *testing.T, issueID, content, key string, parentID *string) (int, CommentResponse) {
	t.Helper()
	body := map[string]any{"content": content, "idempotency_key": key}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), "id", issueID)
	testHandler.CreateComment(w, r)
	var resp CommentResponse
	if w.Code == http.StatusCreated || w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment response: %v", err)
		}
	}
	return w.Code, resp
}

func countCommentsForIssueForTest(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	return count
}

func TestCreateCommentIdempotency(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("single create", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "idempotency single", "", "")
		status, created := createCommentWithKeyForTest(t, issueID, "single", "kap-1061-single", nil)
		if status != http.StatusCreated || created.ID == "" {
			t.Fatalf("first request = %d/%+v, want 201 with comment", status, created)
		}
		if got := countCommentsForIssueForTest(t, issueID); got != 1 {
			t.Fatalf("comment count = %d, want 1", got)
		}
	})

	t.Run("retry same key returns original", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "idempotency retry", "", "")
		status1, first := createCommentWithKeyForTest(t, issueID, "accepted before response loss", "kap-1061-retry", nil)
		status2, retry := createCommentWithKeyForTest(t, issueID, "accepted before response loss", "kap-1061-retry", nil)
		if status1 != http.StatusCreated || status2 != http.StatusOK {
			t.Fatalf("statuses = %d, %d; want 201, 200", status1, status2)
		}
		if retry.ID != first.ID {
			t.Fatalf("retry id = %s, want original %s", retry.ID, first.ID)
		}
		if got := countCommentsForIssueForTest(t, issueID); got != 1 {
			t.Fatalf("comment count = %d, want 1", got)
		}
	})

	t.Run("different keys preserve deliberate repeated content", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "idempotency distinct keys", "", "")
		_, first := createCommentWithKeyForTest(t, issueID, "repeat me", "kap-1061-distinct-a", nil)
		status, second := createCommentWithKeyForTest(t, issueID, "repeat me", "kap-1061-distinct-b", nil)
		if status != http.StatusCreated || first.ID == second.ID {
			t.Fatalf("second request = %d/%+v; want distinct created comment", status, second)
		}
		if got := countCommentsForIssueForTest(t, issueID); got != 2 {
			t.Fatalf("comment count = %d, want 2", got)
		}
	})

	t.Run("same key with different request is rejected", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "idempotency key conflict", "", "")
		status1, _ := createCommentWithKeyForTest(t, issueID, "original", "kap-1061-conflict", nil)
		status2, _ := createCommentWithKeyForTest(t, issueID, "changed", "kap-1061-conflict", nil)
		if status1 != http.StatusCreated || status2 != http.StatusConflict {
			t.Fatalf("statuses = %d, %d; want 201, 409", status1, status2)
		}
		if got := countCommentsForIssueForTest(t, issueID); got != 1 {
			t.Fatalf("comment count = %d, want 1", got)
		}
	})

	t.Run("concurrent duplicate delivery", func(t *testing.T) {
		issueID := createCommentTriggerPreviewIssue(t, "idempotency concurrent", "", "")
		const callers = 8
		statuses := make(chan int, callers)
		ids := make(chan string, callers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				status, resp := createCommentWithKeyForTest(t, issueID, "race", "kap-1061-concurrent", nil)
				statuses <- status
				ids <- resp.ID
			}()
		}
		close(start)
		wg.Wait()
		close(statuses)
		close(ids)

		created := 0
		for status := range statuses {
			if status == http.StatusCreated {
				created++
			} else if status != http.StatusOK {
				t.Errorf("unexpected status %d", status)
			}
		}
		var canonical string
		for id := range ids {
			if canonical == "" {
				canonical = id
			} else if id != canonical {
				t.Errorf("duplicate returned id %s, want %s", id, canonical)
			}
		}
		if created != 1 {
			t.Errorf("created responses = %d, want 1", created)
		}
		if got := countCommentsForIssueForTest(t, issueID); got != 1 {
			t.Fatalf("comment count = %d, want 1", got)
		}
	})
}

func TestCreateCommentIdempotencyRunsMentionSideEffectsOnceAndPreservesReply(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Idempotency Mention Agent", nil)
	issueID := createCommentTriggerPreviewIssue(t, "idempotency mention side effects", "", "")
	parentStatus, parent := createCommentWithKeyForTest(t, issueID, "thread root", "kap-1061-parent", nil)
	if parentStatus != http.StatusCreated {
		t.Fatalf("create parent: status %d", parentStatus)
	}
	content := fmt.Sprintf("[@Agent](mention://agent/%s) handle once", agentID)
	status1, first := createCommentWithKeyForTest(t, issueID, content, "kap-1061-mention", &parent.ID)
	status2, retry := createCommentWithKeyForTest(t, issueID, content, "kap-1061-mention", &parent.ID)
	if status1 != http.StatusCreated || status2 != http.StatusOK || retry.ID != first.ID {
		t.Fatalf("retry result = (%d,%s) then (%d,%s)", status1, first.ID, status2, retry.ID)
	}
	if retry.ParentID == nil || *retry.ParentID != parent.ID {
		t.Fatalf("retry parent = %v, want %s", retry.ParentID, parent.ID)
	}
	if got := countQueuedCommentTriggerTasks(t, issueID, agentID); got != 1 {
		t.Fatalf("mention tasks = %d, want 1", got)
	}
}
