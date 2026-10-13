package handler

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// failCancellationTxStarter begins real transactions whose task cancellation
// statements fail.
type failCancellationTxStarter struct{ delegate *pgxpool.Pool }

type failCancellationTx struct{ pgx.Tx }

func (s failCancellationTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failCancellationTx{Tx: tx}, nil
}

func (t failCancellationTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.Contains(query, "-- name: CancelAgentTasks") {
		return nil, errors.New("injected task cancellation failure")
	}
	return t.Tx.Query(ctx, query, args...)
}

// Deleting a thread removes the root and every reply, whoever wrote them, and
// cancels the tasks those comments triggered in the same transaction.
func TestDeleteCommentThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	member := func(name string) string {
		t.Helper()
		id := dbfx.Insert(t, "user", testutil.Cols{"name": name, "email": name + "@thread-delete.test"})
		dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": id, "role": "member"})
		return id
	}
	author := member("root-author")
	unrelated := member("unrelated-member")
	agentID := dbfx.Agent(t, "thread delete agent", testRuntimeID)

	for _, tt := range []struct {
		name string
		// actor deletes the thread; replies are written by someone else.
		actor string
		// rootDeleted starts from a root an ordinary delete already tombstoned.
		rootDeleted bool
		failCancel  bool
		status      int
	}{
		{name: "root author removes a mixed-author thread", actor: author, status: http.StatusNoContent},
		{name: "root author removes the replies under a deleted root", actor: author, rootDeleted: true, status: http.StatusNoContent},
		{name: "unrelated member cannot remove the thread", actor: unrelated, status: http.StatusForbidden},
		{name: "failed task cancellation keeps the thread", actor: author, failCancel: true, status: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			issue := dbfx.Issue(t, tt.name)
			rootContent, rootCols := "root", testutil.Cols{"author_id": author}
			if tt.rootDeleted {
				rootContent, rootCols["deleted_at"] = "", time.Now()
			}
			root := dbfx.Comment(t, issue, rootContent, rootCols)
			reply := dbfx.Comment(t, issue, "reply", testutil.Cols{"parent_id": root})
			nested := dbfx.Comment(t, issue, "nested reply", testutil.Cols{"parent_id": reply})
			other := dbfx.Comment(t, issue, "other thread")
			task := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issue, "trigger_comment_id": nested})

			h := *testHandler
			if tt.failCancel {
				h.TxStarter = failCancellationTxStarter{delegate: testPool}
			}
			req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+root+"/thread", nil), "commentId", root)
			req.Header.Set("X-User-ID", tt.actor)
			testutil.Call(t, h.DeleteCommentThread, req).Want(tt.status)

			removed := tt.status == http.StatusNoContent
			want, wantTask := []string{other}, "cancelled"
			if !removed {
				want, wantTask = []string{root, reply, nested, other}, "queued"
			}
			entries, status := fetchTimeline(t, issue)
			if status != http.StatusOK {
				t.Fatalf("timeline status = %d, want 200", status)
			}
			var got []string
			for _, entry := range entries {
				got = append(got, entry.ID)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("timeline = %v, want %v", got, want)
			}
			var taskStatus string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, task).Scan(&taskStatus)
			if taskStatus != wantTask {
				t.Errorf("task triggered by the nested reply is %s, want %s", taskStatus, wantTask)
			}
		})
	}
}
