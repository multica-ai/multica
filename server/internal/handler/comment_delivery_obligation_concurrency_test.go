package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentCoverageBarrierDB struct {
	*pgxpool.Pool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *commentCoverageBarrierDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := d.Pool.QueryRow(ctx, sql, args...)
	if strings.HasPrefix(sql, "-- name: CommentDeliveryObligationCoverage") {
		return commentCoverageBarrierRow{Row: row, db: d}
	}
	return row
}

type commentCoverageBarrierRow struct {
	pgx.Row
	db *commentCoverageBarrierDB
}

func (r commentCoverageBarrierRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.db.once.Do(func() {
		close(r.db.entered)
		<-r.db.release
	})
	return err
}

type commentObligationClaimOnWakeup struct {
	pool             *pgxpool.Pool
	issueID, agentID string
	errors           chan error
}

func (n commentObligationClaimOnWakeup) NotifyTaskAvailable(_, taskID string) {
	// Model a daemon claiming the queued row before the other drainer resumes.
	// No process, account or model is invoked.
	_, err := n.pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='running', started_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1 AND issue_id=$2 AND agent_id=$3`, taskID, n.issueID, n.agentID)
	if err != nil {
		n.errors <- err
	}
}

func TestCommentDeliveryObligationConcurrentDrainAndClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, contender := range []string{"second drainer", "original enqueue"} {
		t.Run(contender, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "concurrent obligation runtime")
			agentID := dbfx.Agent(t, "concurrent obligation target", runtimeID)
			issueID := dbfx.Issue(t, "concurrent obligation claim", testutil.Cols{
				"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
			})
			commentID := dbfx.Comment(t, issueID, "One accepted instruction")
			_, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
			})
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
			barrier := &commentCoverageBarrierDB{Pool: testPool, entered: make(chan struct{}), release: make(chan struct{})}
			notifier := commentObligationClaimOnWakeup{pool: testPool, issueID: issueID, agentID: agentID, errors: make(chan error, 4)}
			first := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			second := New(db.New(testPool), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			first.TaskService.Wakeup = notifier
			second.TaskService.Wakeup = notifier
			firstResult := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				firstResult <- first.DrainCommentDeliveryObligations(t.Context(), 50)
			}()
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(barrier.release) })
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("first drainer did not exit")
				}
			})
			select {
			case <-barrier.entered:
			case <-finished:
				t.Fatalf("first drainer exited before reading coverage: %v", <-firstResult)
			case <-time.After(5 * time.Second):
				t.Fatal("first drainer did not reach coverage")
			}
			if contender == "original enqueue" {
				issue, err := second.Queries.GetIssue(t.Context(), parseUUID(issueID))
				if err != nil {
					t.Fatal(err)
				}
				agent, err := second.Queries.GetAgent(t.Context(), parseUUID(agentID))
				if err != nil {
					t.Fatal(err)
				}
				status, _ := second.resolveCommentTriggerEnqueue(t.Context(), issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee}, commentDeliverySourceForTest(t, parseUUID(commentID)))
				if status != DispatchDeferred {
					t.Errorf("competing producer status=%s, want deferred", status)
				}
			} else if err := second.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(barrier.release) })
			if err := <-firstResult; err != nil {
				t.Fatal(err)
			}
			if len(notifier.errors) > 0 {
				t.Fatalf("claim on wakeup: %v", <-notifier.errors)
			}
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND trigger_comment_id=$3`, issueID, agentID, commentID).Scan(&count)
			if count != 1 {
				t.Fatalf("concurrent drain created %d tasks for one instruction, want one", count)
			}
		})
	}
}

func TestCommentDeliveryObligationWithdrawnDuringDrainDoesNotStart(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, action := range []string{"delete comment", "cancel agent", "cancel issue", "committed comment deletion"} {
		t.Run(action, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "withdrawn obligation runtime")
			agentID := dbfx.Agent(t, "withdrawn obligation target", runtimeID)
			issueID := dbfx.Issue(t, "withdrawn during drain", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			commentID := dbfx.Comment(t, issueID, "Withdraw this before dispatch")
			if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
				WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
			})); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
			})
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
			barrier := &commentCoverageBarrierDB{Pool: testPool, entered: make(chan struct{}), release: make(chan struct{})}
			h := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			result := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				result <- h.DrainCommentDeliveryObligations(t.Context(), 50)
			}()
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(barrier.release) })
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("withdrawn-input drainer did not exit")
				}
			})
			select {
			case <-barrier.entered:
			case <-finished:
				t.Fatalf("drain exited before coverage: %v", <-result)
			case <-time.After(5 * time.Second):
				t.Fatal("drain did not reach coverage")
			}
			switch action {
			case "delete comment":
				dbfx.Exec(t, `UPDATE comment SET deleted_at=now(), content='', revision=revision+1 WHERE id=$1`, commentID)
			case "cancel agent":
				if _, err := testHandler.TaskService.CancelTasksForAgent(t.Context(), parseUUID(agentID)); err != nil {
					t.Fatal(err)
				}
			case "cancel issue":
				if err := testHandler.TaskService.CancelTasksForIssue(t.Context(), parseUUID(issueID)); err != nil {
					t.Fatal(err)
				}
			case "committed comment deletion":
				if _, err := testHandler.deleteComment(t.Context(), parseUUID(commentID), parseUUID(testWorkspaceID)); err != nil {
					t.Fatal(err)
				}
			}
			release.Do(func() { close(barrier.release) })
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			var tasks int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&tasks)
			if tasks != 0 {
				t.Fatalf("withdrawn input started %d tasks", tasks)
			}
		})
	}
}
