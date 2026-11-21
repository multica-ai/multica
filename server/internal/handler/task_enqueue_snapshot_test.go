package handler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An INSERT can take its statement snapshot while the binding change is still
// uncommitted, then wait inside the owner fence. It must use the updated tuple
// after that wait, rather than persisting either the snapshot or caller's A.
func TestTaskEnqueueRefreshesBindingAfterLockWait(t *testing.T) {
	for _, kind := range []string{"issue", "deferred_issue", "quick_create", "chat", "autopilot", "wakeup"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			oldRuntime := dbfx.Runtime(t, "enqueue-snapshot-A")
			newRuntime := dbfx.Runtime(t, "enqueue-snapshot-B")
			agentID := dbfx.Agent(t, "enqueue-snapshot-agent", oldRuntime)
			issueID := dbfx.Issue(t, "enqueue binding snapshot")
			chatID := dbfx.ChatSession(t, agentID)
			dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE agent_id=$1", agentID)
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			q := testHandler.Queries.WithTx(tx)
			if _, err := q.LockWorkspaceForChatSessionCreate(ctx, parseUUID(testWorkspaceID)); err != nil {
				t.Fatal(err)
			}
			if _, err := q.LockAgentRuntimeForAccessChange(ctx, parseUUID(newRuntime)); err != nil {
				t.Fatal(err)
			}
			if _, err := q.GetAgentForUpdate(ctx, parseUUID(agentID)); err != nil {
				t.Fatal(err)
			}
			if _, err := q.UpdateAgent(ctx, db.UpdateAgentParams{ID: parseUUID(agentID), RuntimeID: parseUUID(newRuntime)}); err != nil {
				t.Fatal(err)
			}

			type outcome struct {
				task db.AgentTaskQueue
				err  error
			}
			result := make(chan outcome, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				var task db.AgentTaskQueue
				var err error
				a, r, i := parseUUID(agentID), parseUUID(oldRuntime), parseUUID(issueID)
				switch kind {
				case "issue":
					task, err = testHandler.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: a, RuntimeID: r, IssueID: i, Priority: 4})
				case "deferred_issue":
					task, err = testHandler.Queries.CreateDeferredChannelIssueTask(ctx, db.CreateDeferredChannelIssueTaskParams{
						AgentID: a, RuntimeID: r, IssueID: i, Priority: 4,
						FireAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
					})
				case "quick_create":
					task, err = testHandler.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{AgentID: a, RuntimeID: r, Priority: 4, Context: []byte(`{"type":"quick_create"}`)})
				case "chat":
					task, err = testHandler.Queries.CreateChatTask(ctx, db.CreateChatTaskParams{AgentID: a, RuntimeID: r, ChatSessionID: parseUUID(chatID), Priority: 4})
				case "autopilot":
					task, err = testHandler.Queries.CreateAutopilotTask(ctx, db.CreateAutopilotTaskParams{AgentID: a, RuntimeID: r, Priority: 4})
				case "wakeup":
					task, err = testHandler.Queries.CreateWakeupTask(ctx, db.CreateWakeupTaskParams{AgentID: a, RuntimeID: r, IssueID: i, Priority: 4, WakeupContext: []byte(`{}`)})
				}
				result <- outcome{task, err}
			}()
			defer func() { tx.Rollback(context.Background()); cancel(); <-done }()
			for {
				select {
				case got := <-result:
					t.Fatalf("write completed before binding lock was released: %v", got.err)
				default:
				}
				var blocked bool
				if err := testPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))", tx.Conn().PgConn().PID()).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case <-time.After(5 * time.Millisecond):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			got := <-result
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.task.RuntimeID != parseUUID(newRuntime) || got.task.Priority != 4 {
				t.Fatalf("task runtime=%s priority=%d, want B=%s priority=4", uuidToString(got.task.RuntimeID), got.task.Priority, newRuntime)
			}
		})
	}
}
