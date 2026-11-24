package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Two clicks on the same emoji land at once. The second request queues behind
// the first one's uncommitted reaction and must return that reaction once the
// first commits, not fail because its own snapshot predates the row.
func TestAddIssueReactionQueuedBehindSameReactionReturnsIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := dbfx.Issue(t, "same reaction twice at once")

	first, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first: %v", err)
	}
	defer first.Rollback(ctx)
	var firstPID int
	if err := first.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&firstPID); err != nil {
		t.Fatalf("first pid: %v", err)
	}
	added, err := testHandler.Queries.WithTx(first).AddIssueReaction(ctx, db.AddIssueReactionParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		ActorType:   "member",
		ActorID:     parseUUID(testUserID),
		Emoji:       "👍",
	})
	if err != nil {
		t.Fatalf("first add: %v", err)
	}

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.AddIssueReaction(w, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/reactions", map[string]any{
			"emoji": "👍",
		}), "id", issueID))
	}()
	waitForIssueReactionBackendBlockedBy(t, firstPID, done)

	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	<-done

	if w.Code != http.StatusCreated {
		t.Fatalf("second add = %d %s, want 201", w.Code, w.Body.String())
	}
	var got IssueReactionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != uuidToString(added.ID) {
		t.Fatalf("second add returned reaction %s, want the first one %s", got.ID, uuidToString(added.ID))
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_reaction WHERE issue_id = $1`, issueID); n != 1 {
		t.Fatalf("issue has %d reactions, want 1", n)
	}
}

func waitForIssueReactionBackendBlockedBy(t *testing.T, holderPID int, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatal("request finished before the competing transaction committed")
		default:
		}
		var blocked bool
		if err := testPool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND $1 = ANY(pg_blocking_pids(pid))
			)
		`, holderPID).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked request: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("request did not queue behind the competing transaction")
}
