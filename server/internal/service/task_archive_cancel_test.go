package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func newArchiveCancelFixture(t *testing.T) (*testutil.Fixture, string, string) {
	t.Helper()
	fx := testutil.New(newCancelFinalizePool(t), "", "")
	suffix := time.Now().UnixNano()
	fx.UserID = fx.User(t, "Archive cancellation", fmt.Sprintf("archive-cancel-%d@multica.test", suffix))
	fx.WorkspaceID = fx.Workspace(t, "Archive cancellation", fmt.Sprintf("archive-cancel-%d", suffix))
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Archive cancellation runtime")
	agentID := fx.Agent(t, "Archived agent", runtimeID)
	return fx, agentID, runtimeID
}

func TestCancelTasksForArchivedAgentPublishesCommittedChatCancellations(t *testing.T) {
	ctx := context.Background()
	fx, agentID, runtimeID := newArchiveCancelFixture(t)
	chatByTask := make(map[string]string)
	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred"} {
		// No channel binding: the lifecycle notification applies to every chat.
		chatID := fx.ChatSession(t, agentID)
		taskID := fx.Task(t, agentID, testutil.Cols{
			"chat_session_id": chatID,
			"runtime_id":      runtimeID,
			"status":          status,
		})
		chatByTask[taskID] = chatID
	}
	issueID := fx.Issue(t, "Issue task remains part of bulk cancellation")
	issueTaskID := fx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "status": "running",
	})
	unchanged := make(map[string]string)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		unchanged[fx.Task(t, agentID, testutil.Cols{
			"chat_session_id": fx.ChatSession(t, agentID),
			"runtime_id":      runtimeID,
			"status":          status,
		})] = status
	}
	otherAgentID := fx.Agent(t, "Other agent", runtimeID)
	unchanged[fx.Task(t, otherAgentID, testutil.Cols{
		"chat_session_id": fx.ChatSession(t, otherAgentID),
		"runtime_id":      runtimeID,
		"status":          "running",
	})] = "running"

	// Hold a separate connection before cancellation starts, so the observer
	// cannot reuse the transaction connection or see its uncommitted writes.
	observer, err := fx.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire event observer: %v", err)
	}
	defer observer.Release()
	bus := events.New()
	seen := make(map[string]int)
	bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		seen[e.TaskID]++
		chatID, ok := chatByTask[e.TaskID]
		if !ok {
			t.Errorf("unexpected cancellation event for task %q", e.TaskID)
			return
		}
		if e.WorkspaceID != fx.WorkspaceID || e.ChatSessionID != chatID || e.ActorType != "system" {
			t.Errorf("task %s event scope = workspace %q chat %q actor %q", e.TaskID, e.WorkspaceID, e.ChatSessionID, e.ActorType)
		}
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Errorf("task %s payload type = %T, want map[string]any", e.TaskID, e.Payload)
			return
		}
		for key, want := range map[string]any{
			"task_id": e.TaskID, "agent_id": agentID, "chat_session_id": chatID,
			"issue_id": "", "status": "cancelled",
		} {
			if got := payload[key]; got != want {
				t.Errorf("task %s payload[%q] = %v, want %v", e.TaskID, key, got, want)
			}
		}
		var status string
		if err := observer.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, e.TaskID).Scan(&status); err != nil {
			t.Errorf("read cancellation from event observer: %v", err)
		} else if status != "cancelled" {
			t.Errorf("task %s event arrived before cancellation committed: status = %q", e.TaskID, status)
		}
	})
	svc := NewTaskService(db.New(fx.Pool), fx.Pool, nil, bus)
	cancelled, err := svc.CancelTasksForArchivedAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("cancel archived agent tasks: %v", err)
	}
	if len(cancelled) != len(chatByTask)+1 {
		t.Errorf("cancelled rows = %d, want %d chat and issue tasks", len(cancelled), len(chatByTask)+1)
	}
	returned := make(map[string]int)
	for _, task := range cancelled {
		id := util.UUIDToString(task.ID)
		returned[id]++
		if _, ok := chatByTask[id]; !ok && id != issueTaskID {
			t.Errorf("unexpected cancelled row %s", id)
		}
		if task.Status != "cancelled" {
			t.Errorf("returned task %s status = %q, want cancelled", id, task.Status)
		}
	}
	for taskID := range chatByTask {
		if returned[taskID] != 1 || seen[taskID] != 1 {
			t.Errorf("chat task %s: returned %d times, published %d times; want one each", taskID, returned[taskID], seen[taskID])
		}
	}
	if returned[issueTaskID] != 1 || seen[issueTaskID] != 0 {
		t.Errorf("issue task: returned %d times, published %d times; want one row and no task event", returned[issueTaskID], seen[issueTaskID])
	}
	if len(seen) != len(chatByTask) {
		t.Errorf("distinct event tasks = %d, want %d", len(seen), len(chatByTask))
	}
	for taskID := range returned {
		unchanged[taskID] = "cancelled"
	}
	for taskID, want := range unchanged {
		var status string
		if err := observer.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
			t.Fatalf("read task %s: %v", taskID, err)
		}
		if status != want {
			t.Errorf("task %s persisted status = %q, want %q", taskID, status, want)
		}
	}

	beforeRepeat := make(map[string]int, len(seen))
	for taskID, count := range seen {
		beforeRepeat[taskID] = count
	}
	cancelled, err = svc.CancelTasksForArchivedAgent(ctx, util.MustParseUUID(agentID))
	if err != nil || len(cancelled) != 0 {
		t.Fatalf("repeat cancellation: rows = %d, error = %v; want no rows and no error", len(cancelled), err)
	}
	if len(seen) != len(beforeRepeat) {
		t.Error("repeat cancellation published events for additional tasks")
	}
	for taskID, count := range seen {
		if count != beforeRepeat[taskID] {
			t.Errorf("repeat cancellation published another event for task %s", taskID)
		}
	}
}

type archiveCancelRejectCommit struct {
	pool             *pgxpool.Pool
	taskID           string
	err              error
	statusBeforeFail string
}

func (s *archiveCancelRejectCommit) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &archiveCancelRejectCommitTx{Tx: tx, starter: s}, nil
}

type archiveCancelRejectCommitTx struct {
	pgx.Tx
	starter *archiveCancelRejectCommit
}

func (tx *archiveCancelRejectCommitTx) Commit(ctx context.Context) error {
	if err := tx.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, tx.starter.taskID).Scan(&tx.starter.statusBeforeFail); err != nil {
		return err
	}
	if err := tx.Tx.Rollback(ctx); err != nil {
		return err
	}
	return tx.starter.err
}

func TestCancelTasksForArchivedAgentCommitFailureDoesNotPublish(t *testing.T) {
	ctx := context.Background()
	fx, agentID, runtimeID := newArchiveCancelFixture(t)
	taskID := fx.Task(t, agentID, testutil.Cols{
		"chat_session_id": fx.ChatSession(t, agentID), "runtime_id": runtimeID, "status": "running",
	})
	injectedErr := errors.New("injected archive cancellation commit failure")
	starter := &archiveCancelRejectCommit{pool: fx.Pool, taskID: taskID, err: injectedErr}
	bus := events.New()
	var published int
	bus.Subscribe(protocol.EventTaskCancelled, func(events.Event) { published++ })
	svc := NewTaskService(db.New(fx.Pool), starter, nil, bus)

	cancelled, err := svc.CancelTasksForArchivedAgent(ctx, util.MustParseUUID(agentID))
	if !errors.Is(err, injectedErr) || cancelled != nil {
		t.Errorf("failed cancellation: rows = %v, error = %v; want nil rows and injected error", cancelled, err)
	}
	if starter.statusBeforeFail != "cancelled" {
		t.Errorf("status inside failed transaction = %q, want cancelled", starter.statusBeforeFail)
	}
	var persistedStatus string
	if err := fx.Pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&persistedStatus); err != nil {
		t.Fatalf("read rolled-back task: %v", err)
	}
	if persistedStatus != "running" || published != 0 {
		t.Errorf("failed cancellation: persisted status = %q, published = %d; want running and no events", persistedStatus, published)
	}
}
