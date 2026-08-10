package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type runtimePoolSeamScheduler struct {
	assignResult runtimepool.AssignResult
	assignErr    error
	sweepResults []runtimepool.AssignResult
	sweepErr     error
	assignCalls  []runtimepool.AssignRequest
	sweepLimits  []int
	returned     bool
}

func (s *runtimePoolSeamScheduler) AssignWaiting(_ context.Context, request runtimepool.AssignRequest) (runtimepool.AssignResult, error) {
	s.assignCalls = append(s.assignCalls, request)
	s.returned = true
	return s.assignResult, s.assignErr
}

func (s *runtimePoolSeamScheduler) SweepWaiting(_ context.Context, limit int) ([]runtimepool.AssignResult, error) {
	s.sweepLimits = append(s.sweepLimits, limit)
	s.returned = true
	return s.sweepResults, s.sweepErr
}

type runtimePoolWakeRecorder struct {
	calls [][2]string
}

func (r *runtimePoolWakeRecorder) NotifyTaskAvailable(runtimeID, taskID string) {
	r.calls = append(r.calls, [2]string{runtimeID, taskID})
}

func TestRuntimePoolSeamAssignmentPublishesAfterReturnExactlyOnce(t *testing.T) {
	workspaceID := "10000000-0000-4000-8000-000000000001"
	assigned := runtimePoolSeamTask(1, workspaceID, "queued", true)
	waiting := runtimePoolSeamTask(2, workspaceID, runtimepool.StatusWaitingRuntime, false)
	scheduler := &runtimePoolSeamScheduler{assignResult: runtimepool.AssignResult{
		Assigned:        []db.AgentTaskQueue{assigned},
		PromotedWaiting: []db.AgentTaskQueue{waiting},
	}}
	bus := events.New()
	wakeup := &runtimePoolWakeRecorder{}
	var eventTypes []string
	bus.SubscribeAll(func(event events.Event) {
		if !scheduler.returned {
			t.Fatal("task event published before scheduler returned")
		}
		eventTypes = append(eventTypes, event.Type)
	})
	svc := &TaskService{Bus: bus, Wakeup: wakeup, RuntimePool: scheduler}

	workspace := runtimePoolSeamUUID(10)
	focus := waiting.ID
	result, err := svc.AssignPoolWorkspace(context.Background(), workspace, focus)
	if err != nil {
		t.Fatalf("AssignPoolWorkspace: %v", err)
	}
	if len(result.Assigned) != 1 || len(result.PromotedWaiting) != 1 {
		t.Fatalf("result = %+v, want one assigned and one promoted waiting", result)
	}
	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("assign calls = %d, want 1", len(scheduler.assignCalls))
	}
	request := scheduler.assignCalls[0]
	if request.WorkspaceID != workspace || request.FocusTaskID != focus || request.Limit != runtimepool.AssignmentBatchLimit {
		t.Fatalf("assign request = %+v", request)
	}
	if fmt.Sprint(eventTypes) != fmt.Sprint([]string{protocol.EventTaskQueued, protocol.EventTaskWaitingRuntime}) {
		t.Fatalf("events = %v, want queued then waiting_runtime", eventTypes)
	}
	if len(wakeup.calls) != 1 {
		t.Fatalf("daemon wakeups = %v, want exactly one", wakeup.calls)
	}
}

func TestRuntimePoolSeamFocusedUnassignedTaskPublishesWaitingExactlyOnce(t *testing.T) {
	pool := pooltestdb.Open(t)
	tests := []struct {
		name      string
		mutate    func(context.Context, *runtimePoolFocusFixture) error
		selectIDs func(context.Context, *runtimePoolFocusFixture) (pgtype.UUID, pgtype.UUID, error)
		cancelCtx bool
		wantEvent bool
		wantErr   string
	}{
		{name: "persisted waiting task", wantEvent: true},
		{
			name: "queued task",
			mutate: func(ctx context.Context, fixture *runtimePoolFocusFixture) error {
				_, err := fixture.tx.Exec(ctx, `
					UPDATE agent_task_queue SET status = 'queued', runtime_id = $1 WHERE id = $2
				`, fixture.runtimeID, fixture.taskID)
				return err
			},
		},
		{
			name: "cancelled task",
			mutate: func(ctx context.Context, fixture *runtimePoolFocusFixture) error {
				_, err := fixture.tx.Exec(ctx, `
					UPDATE agent_task_queue SET status = 'cancelled', completed_at = now() WHERE id = $1
				`, fixture.taskID)
				return err
			},
		},
		{
			name: "task not found",
			selectIDs: func(ctx context.Context, fixture *runtimePoolFocusFixture) (pgtype.UUID, pgtype.UUID, error) {
				var missing pgtype.UUID
				err := fixture.tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&missing)
				return fixture.workspaceID, missing, err
			},
			wantErr: "no rows",
		},
		{
			name: "wrong workspace",
			selectIDs: func(ctx context.Context, fixture *runtimePoolFocusFixture) (pgtype.UUID, pgtype.UUID, error) {
				var otherWorkspace pgtype.UUID
				err := fixture.tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&otherWorkspace)
				return otherWorkspace, fixture.taskID, err
			},
			wantErr: "workspace",
		},
		{name: "focus lookup error", cancelCtx: true, wantErr: context.Canceled.Error()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimePoolFocusFixture(t, pool)
			ctx := context.Background()
			if test.mutate != nil {
				if err := test.mutate(ctx, fixture); err != nil {
					t.Fatalf("mutate focus task: %v", err)
				}
			}
			workspaceID, focusTaskID := fixture.workspaceID, fixture.taskID
			if test.selectIDs != nil {
				var err error
				workspaceID, focusTaskID, err = test.selectIDs(ctx, fixture)
				if err != nil {
					t.Fatalf("select test ids: %v", err)
				}
			}
			if test.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			scheduler := &runtimePoolSeamScheduler{}
			bus := events.New()
			var seen []events.Event
			bus.SubscribeAll(func(event events.Event) {
				if !scheduler.returned {
					t.Fatal("waiting event published before scheduler returned")
				}
				seen = append(seen, event)
			})
			svc := &TaskService{Queries: db.New(fixture.tx), Bus: bus, RuntimePool: scheduler}

			_, err := svc.AssignPoolWorkspace(ctx, workspaceID, focusTaskID)
			if test.wantErr == "" && err != nil {
				t.Fatalf("AssignPoolWorkspace: %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("AssignPoolWorkspace error = %v, want containing %q", err, test.wantErr)
			}
			if got := len(seen); got != boolCount(test.wantEvent) {
				t.Fatalf("events = %d, want %d", got, boolCount(test.wantEvent))
			}
			if !test.wantEvent {
				return
			}
			event := seen[0]
			if event.Type != protocol.EventTaskWaitingRuntime || event.TaskID != util.UUIDToString(fixture.taskID) || event.WorkspaceID != util.UUIDToString(fixture.workspaceID) {
				t.Fatalf("waiting event = %+v", event)
			}
			payload, ok := event.Payload.(map[string]any)
			if !ok {
				t.Fatalf("waiting payload type = %T", event.Payload)
			}
			if payload["task_id"] != util.UUIDToString(fixture.taskID) || payload["agent_id"] != util.UUIDToString(fixture.agentID) || payload["issue_id"] != util.UUIDToString(fixture.issueID) || payload["status"] != runtimepool.StatusWaitingRuntime {
				t.Fatalf("waiting payload = %#v", payload)
			}
		})
	}
}

func TestRuntimePoolSeamWakeUsesWorkspaceBoundWithoutFocus(t *testing.T) {
	scheduler := &runtimePoolSeamScheduler{}
	svc := &TaskService{Bus: events.New(), RuntimePool: scheduler}
	workspace := runtimePoolSeamUUID(14)

	if err := svc.WakePoolWorkspace(context.Background(), workspace); err != nil {
		t.Fatalf("WakePoolWorkspace: %v", err)
	}
	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("assign calls = %d, want 1", len(scheduler.assignCalls))
	}
	request := scheduler.assignCalls[0]
	if request.WorkspaceID != workspace || request.FocusTaskID.Valid || request.Limit != runtimepool.AssignmentBatchLimit {
		t.Fatalf("wake request = %+v", request)
	}
}

// Break caught: a terminal Task frees Runtime/Agent serialization capacity,
// but only waking the old Runtime daemon leaves Workspace Pool waiters asleep.
func TestRuntimePoolWakeAfterTerminalTask(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			scheduler := &runtimePoolSeamScheduler{}
			svc := &TaskService{RuntimePool: scheduler}
			task := runtimePoolSeamTask(22, "", status, true)
			task.RuntimeBindingMode = runtimepool.BindingPool
			task.PlacementWorkspaceID = runtimePoolSeamUUID(23)

			svc.NotifyTaskFinished(task)

			if len(scheduler.assignCalls) != 1 {
				t.Fatalf("Pool wake calls = %d; want 1", len(scheduler.assignCalls))
			}
			request := scheduler.assignCalls[0]
			if request.WorkspaceID != task.PlacementWorkspaceID || request.FocusTaskID.Valid || request.Limit != runtimepool.AssignmentBatchLimit {
				t.Fatalf("Pool wake request = %+v", request)
			}
		})
	}
}

// Break caught: a fixed Task has no placement Workspace snapshot, but while it
// was capacity-bearing it could still block first-time Pool placement on its
// Runtime. Its terminal transition must resolve and wake that Runtime's
// Workspace without changing fixed task semantics.
func TestRuntimePoolWakeAfterFixedTerminalTask(t *testing.T) {
	fixture := newRuntimePoolFocusFixture(t, pooltestdb.Open(t))
	scheduler := &runtimePoolSeamScheduler{}
	svc := &TaskService{Queries: db.New(fixture.tx), RuntimePool: scheduler}

	svc.NotifyTaskFinished(db.AgentTaskQueue{
		ID:                 runtimePoolSeamUUID(24),
		RuntimeID:          fixture.runtimeID,
		RuntimeBindingMode: runtimepool.BindingFixed,
		Status:             "completed",
	})

	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("Pool wake calls = %d; want 1", len(scheduler.assignCalls))
	}
	if scheduler.assignCalls[0].WorkspaceID != fixture.workspaceID {
		t.Fatalf("Pool wake Workspace = %v; want %v", scheduler.assignCalls[0].WorkspaceID, fixture.workspaceID)
	}
}

// Break caught: bulk cancellation/failure can finish several fixed and Pool
// Tasks in one Workspace. Running the allocator once per row would duplicate
// events and work; omitting the batch wake would leave capacity idle.
func TestRuntimePoolWakeBatchDedupesWorkspace(t *testing.T) {
	fixture := newRuntimePoolFocusFixture(t, pooltestdb.Open(t))
	scheduler := &runtimePoolSeamScheduler{}
	svc := &TaskService{Queries: db.New(fixture.tx), RuntimePool: scheduler}
	poolOne := runtimePoolSeamTask(25, "", "cancelled", true)
	poolOne.RuntimeBindingMode = runtimepool.BindingPool
	poolOne.PlacementWorkspaceID = fixture.workspaceID
	poolTwo := runtimePoolSeamTask(26, "", "failed", true)
	poolTwo.RuntimeBindingMode = runtimepool.BindingPool
	poolTwo.PlacementWorkspaceID = fixture.workspaceID
	fixed := db.AgentTaskQueue{
		ID:                 runtimePoolSeamUUID(27),
		RuntimeID:          fixture.runtimeID,
		RuntimeBindingMode: runtimepool.BindingFixed,
		Status:             "completed",
	}

	svc.notifyTasksFinished([]db.AgentTaskQueue{poolOne, poolTwo, fixed})

	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("Pool wake calls = %d; want 1 for one Workspace", len(scheduler.assignCalls))
	}
	if scheduler.assignCalls[0].WorkspaceID != fixture.workspaceID {
		t.Fatalf("Pool wake Workspace = %v; want %v", scheduler.assignCalls[0].WorkspaceID, fixture.workspaceID)
	}
}

// Break caught: escalation cleanup is an autocommit cancellation path. Once a
// queued Pool fallback releases its Runtime, the Workspace allocator must run
// immediately rather than waiting for the periodic sweep.
func TestRuntimePoolWakeAfterDeferredEscalationCancellation(t *testing.T) {
	pool := pooltestdb.Open(t)
	for _, test := range []struct {
		name   string
		cancel func(context.Context, *TaskService, *runtimePoolFocusFixture)
	}{
		{
			name: "primary task acknowledged",
			cancel: func(ctx context.Context, svc *TaskService, fixture *runtimePoolFocusFixture) {
				svc.cancelDeferredEscalationsForTask(ctx, fixture.taskID)
			},
		},
		{
			name: "agent comment acknowledged",
			cancel: func(ctx context.Context, svc *TaskService, fixture *runtimePoolFocusFixture) {
				svc.CancelDeferredEscalationsForIssueAgent(ctx, fixture.issueID, fixture.agentID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimePoolFocusFixture(t, pool)
			ctx := context.Background()
			fallbackID := addQueuedPoolEscalation(t, ctx, fixture)
			scheduler := &runtimePoolSeamScheduler{}
			svc := &TaskService{
				Queries:     db.New(fixture.tx),
				Bus:         events.New(),
				RuntimePool: scheduler,
			}

			test.cancel(ctx, svc, fixture)

			var status string
			if err := fixture.tx.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fallbackID).Scan(&status); err != nil {
				t.Fatalf("load cancelled fallback: %v", err)
			}
			if status != "cancelled" {
				t.Fatalf("fallback status = %q, want cancelled", status)
			}
			if len(scheduler.assignCalls) != 1 {
				t.Fatalf("Workspace allocator calls = %d, want 1", len(scheduler.assignCalls))
			}
			request := scheduler.assignCalls[0]
			if request.WorkspaceID != fixture.workspaceID || request.FocusTaskID.Valid {
				t.Fatalf("Workspace allocator request = %+v, want nonfocused fixture Workspace", request)
			}
		})
	}
}

// Break caught: RerunIssue commits cancellation before it attempts to enqueue
// the replacement. The released Pool capacity must wake even when that later
// enqueue fails, and a successful rerun must not need a different wake path.
func TestRuntimePoolWakeAfterRerunCancellation(t *testing.T) {
	pool := pooltestdb.Open(t)
	for _, test := range []struct {
		name        string
		makeFixed   bool
		wantRerunOK bool
	}{
		{name: "replacement enqueue fails"},
		{name: "replacement enqueue succeeds", makeFixed: true, wantRerunOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimePoolFocusFixture(t, pool)
			ctx := context.Background()
			if test.makeFixed {
				if _, err := fixture.tx.Exec(ctx, `
					UPDATE agent
					SET runtime_binding_mode = 'fixed', runtime_mode = 'local', runtime_id = $1
					WHERE id = $2
				`, fixture.runtimeID, fixture.agentID); err != nil {
					t.Fatalf("make rerun target fixed: %v", err)
				}
			}
			scheduler := &runtimePoolSeamScheduler{}
			svc := &TaskService{
				Queries:     db.New(fixture.tx),
				Bus:         events.New(),
				RuntimePool: scheduler,
			}

			_, err := svc.RerunIssue(ctx, fixture.issueID, fixture.taskID, pgtype.UUID{}, fixture.userID, nil)
			if test.wantRerunOK && err != nil {
				t.Fatalf("RerunIssue: %v", err)
			}
			if !test.wantRerunOK && err == nil {
				t.Fatal("RerunIssue unexpectedly succeeded with Pool Agent enqueue still intentionally unsupported before Task8")
			}
			var status string
			if err := fixture.tx.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&status); err != nil {
				t.Fatalf("load rerun source: %v", err)
			}
			if status != "cancelled" {
				t.Fatalf("rerun source status = %q, want cancelled", status)
			}
			if len(scheduler.assignCalls) != 1 {
				t.Fatalf("Workspace allocator calls = %d, want 1", len(scheduler.assignCalls))
			}
			request := scheduler.assignCalls[0]
			if request.WorkspaceID != fixture.workspaceID || request.FocusTaskID.Valid {
				t.Fatalf("Workspace allocator request = %+v, want nonfocused fixture Workspace", request)
			}
		})
	}
}

// Break caught: daemon terminal callbacks are replayable. Only the call that
// actually commits running -> terminal may wake Pool placement; an idempotent
// replay that reads the existing terminal row must not repeat the wake.
func TestRuntimePoolTerminalReplayWakesOnce(t *testing.T) {
	pool := pooltestdb.Open(t)
	for _, test := range []struct {
		name       string
		transition func(context.Context, *TaskService, pgtype.UUID) (*db.AgentTaskQueue, error)
		wantStatus string
	}{
		{
			name: "complete",
			transition: func(ctx context.Context, svc *TaskService, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
				return svc.CompleteTask(ctx, taskID, []byte(`{"output":""}`), "", "", false, "")
			},
			wantStatus: "completed",
		},
		{
			name: "fail",
			transition: func(ctx context.Context, svc *TaskService, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
				return svc.FailTask(ctx, taskID, "terminal replay fixture", "", "", "agent_error.unknown", false, "")
			},
			wantStatus: "failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimePoolFocusFixture(t, pool)
			ctx := context.Background()
			if _, err := fixture.tx.Exec(ctx, `
				UPDATE agent_task_queue
				SET status = 'running', runtime_id = $1, dispatched_at = now(), started_at = now()
				WHERE id = $2
			`, fixture.runtimeID, fixture.taskID); err != nil {
				t.Fatalf("make Pool Task running: %v", err)
			}
			scheduler := &runtimePoolSeamScheduler{}
			svc := &TaskService{
				Queries:     db.New(fixture.tx),
				Bus:         events.New(),
				RuntimePool: scheduler,
			}

			first, err := test.transition(ctx, svc, fixture.taskID)
			if err != nil {
				t.Fatalf("first terminal transition: %v", err)
			}
			if first.Status != test.wantStatus {
				t.Fatalf("first status = %q, want %q", first.Status, test.wantStatus)
			}
			if len(scheduler.assignCalls) != 1 {
				t.Fatalf("allocator calls after first transition = %d, want 1", len(scheduler.assignCalls))
			}

			replayed, err := test.transition(ctx, svc, fixture.taskID)
			if err != nil {
				t.Fatalf("terminal replay: %v", err)
			}
			if replayed.Status != test.wantStatus {
				t.Fatalf("replayed status = %q, want %q", replayed.Status, test.wantStatus)
			}
			if len(scheduler.assignCalls) != 1 {
				t.Fatalf("allocator calls after replay = %d, want total 1", len(scheduler.assignCalls))
			}
			request := scheduler.assignCalls[0]
			if request.WorkspaceID != fixture.workspaceID || request.FocusTaskID.Valid {
				t.Fatalf("Workspace allocator request = %+v, want nonfocused fixture Workspace", request)
			}
		})
	}
}

func TestRuntimePoolSeamEmptyNonfocusedResultPublishesNothing(t *testing.T) {
	scheduler := &runtimePoolSeamScheduler{}
	bus := events.New()
	var eventsSeen int
	bus.SubscribeAll(func(events.Event) { eventsSeen++ })
	svc := &TaskService{Bus: bus, RuntimePool: scheduler}

	if _, err := svc.AssignPoolWorkspace(context.Background(), runtimePoolSeamUUID(20), pgtype.UUID{}); err != nil {
		t.Fatalf("AssignPoolWorkspace: %v", err)
	}
	if eventsSeen != 0 {
		t.Fatalf("events = %d, want 0", eventsSeen)
	}
}

func TestRuntimePoolSeamNilSchedulerFailsClosed(t *testing.T) {
	svc := &TaskService{}
	if _, err := svc.AssignPoolWorkspace(context.Background(), runtimePoolSeamUUID(30), runtimePoolSeamUUID(31)); err == nil {
		t.Fatal("AssignPoolWorkspace with nil scheduler succeeded")
	}
	if err := svc.WakePoolWorkspace(context.Background(), runtimePoolSeamUUID(30)); err == nil {
		t.Fatal("WakePoolWorkspace with nil scheduler succeeded")
	}
}

func TestRuntimePoolSeamSweepUsesSharedPostCommitPathAndBound(t *testing.T) {
	workspaceID := "10000000-0000-4000-8000-000000000002"
	assigned := runtimePoolSeamTask(3, workspaceID, "queued", true)
	waiting := runtimePoolSeamTask(4, workspaceID, runtimepool.StatusWaitingRuntime, false)
	sentinel := errors.New("later workspace failed")
	scheduler := &runtimePoolSeamScheduler{
		sweepResults: []runtimepool.AssignResult{{
			Assigned:        []db.AgentTaskQueue{assigned},
			PromotedWaiting: []db.AgentTaskQueue{waiting},
		}},
		sweepErr: sentinel,
	}
	bus := events.New()
	var eventTypes []string
	bus.SubscribeAll(func(event events.Event) {
		if !scheduler.returned {
			t.Fatal("sweep event published before scheduler returned")
		}
		eventTypes = append(eventTypes, event.Type)
	})
	svc := &TaskService{Bus: bus, RuntimePool: scheduler}

	if err := svc.SweepRuntimePool(context.Background(), 32); !errors.Is(err, sentinel) {
		t.Fatalf("SweepRuntimePool error = %v, want %v", err, sentinel)
	}
	if fmt.Sprint(scheduler.sweepLimits) != "[32]" {
		t.Fatalf("sweep limits = %v, want [32]", scheduler.sweepLimits)
	}
	if fmt.Sprint(eventTypes) != fmt.Sprint([]string{protocol.EventTaskQueued, protocol.EventTaskWaitingRuntime}) {
		t.Fatalf("events = %v, want queued then waiting_runtime", eventTypes)
	}
}

func TestWaitingRuntimeEventAndPoolBoundsAreStable(t *testing.T) {
	if protocol.EventTaskWaitingRuntime != "task:waiting_runtime" {
		t.Fatalf("EventTaskWaitingRuntime = %q", protocol.EventTaskWaitingRuntime)
	}
	if runtimepool.WaitingTaskScanLimit != 64 || runtimepool.RuntimeScanLimit != 128 || runtimepool.AssignmentBatchLimit != 8 {
		t.Fatalf("pool bounds = waiting %d runtime %d assignment %d", runtimepool.WaitingTaskScanLimit, runtimepool.RuntimeScanLimit, runtimepool.AssignmentBatchLimit)
	}
}

func TestRuntimePoolMediaReadyIssuePromotesToWaitingAndUsesFocus(t *testing.T) {
	fixture := newRuntimePoolFocusFixture(t, pooltestdb.Open(t))
	ctx := context.Background()
	if _, err := fixture.tx.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'deferred', fire_at = now() + interval '1 minute', runtime_id = NULL
		WHERE id = $1
	`, fixture.taskID); err != nil {
		t.Fatalf("defer Pool Issue Task: %v", err)
	}

	scheduler := &runtimePoolSeamScheduler{}
	bus := events.New()
	var seen []events.Event
	bus.SubscribeAll(func(event events.Event) { seen = append(seen, event) })
	svc := &TaskService{Queries: db.New(fixture.tx), Bus: bus, RuntimePool: scheduler}

	if err := svc.PromoteDeferredChannelIssueTask(ctx, fixture.taskID); err != nil {
		t.Fatalf("PromoteDeferredChannelIssueTask: %v", err)
	}
	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.assignCalls))
	}
	request := scheduler.assignCalls[0]
	if request.WorkspaceID != fixture.workspaceID || request.FocusTaskID != fixture.taskID || request.Limit != runtimepool.AssignmentBatchLimit {
		t.Fatalf("allocator request = %+v, want Workspace/focus/limit", request)
	}
	if len(seen) != 1 || seen[0].Type != protocol.EventTaskWaitingRuntime || seen[0].TaskID != util.UUIDToString(fixture.taskID) {
		t.Fatalf("events = %+v, want one focused waiting_runtime", seen)
	}
	var status string
	var runtimeID pgtype.UUID
	var fireAt pgtype.Timestamptz
	var waitReason pgtype.Text
	if err := fixture.tx.QueryRow(ctx, `
		SELECT status, runtime_id, fire_at, wait_reason
		FROM agent_task_queue WHERE id = $1
	`, fixture.taskID).Scan(&status, &runtimeID, &fireAt, &waitReason); err != nil {
		t.Fatalf("load promoted Pool Issue Task: %v", err)
	}
	if status != runtimepool.StatusWaitingRuntime || runtimeID.Valid || fireAt.Valid || !waitReason.Valid || waitReason.String != "no_eligible_runtime" {
		t.Fatalf("promoted Pool Issue = (%q,%v,%v,%v), want waiting_runtime/NULL/NULL/no_eligible_runtime", status, runtimeID, fireAt, waitReason)
	}
}

func TestRuntimePoolMediaReadyChatCallsAllocatorOnceAndPublishesEveryWaitingTask(t *testing.T) {
	fixture := newRuntimePoolFocusFixture(t, pooltestdb.Open(t))
	ctx := context.Background()
	var chatSessionID pgtype.UUID
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, fixture.workspaceID, fixture.agentID, fixture.userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create Pool media Chat Session: %v", err)
	}
	if _, err := fixture.tx.Exec(ctx, `
		UPDATE agent_task_queue
		SET issue_id = NULL, chat_session_id = $1, status = 'deferred',
		    fire_at = now() + interval '1 minute', runtime_id = NULL
		WHERE id = $2
	`, chatSessionID, fixture.taskID); err != nil {
		t.Fatalf("convert first Pool media Chat Task: %v", err)
	}
	var secondTaskID pgtype.UUID
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, fire_at, runtime_binding_mode,
			placement_workspace_id, runtime_requester_user_id, session_affinity_state
		) VALUES ($1, $2, NULL, 'deferred', now() + interval '1 minute', 'pool', $3, $4, 'none')
		RETURNING id
	`, fixture.agentID, chatSessionID, fixture.workspaceID, fixture.userID).Scan(&secondTaskID); err != nil {
		t.Fatalf("create second Pool media Chat Task: %v", err)
	}
	var unresolvedTaskID pgtype.UUID
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, fire_at, runtime_binding_mode,
			placement_workspace_id, runtime_requester_user_id, session_affinity_state, wait_reason
		) VALUES (
			$1, $2, NULL, 'deferred', now() + interval '1 minute', 'pool', $3, $4,
			'unresolved', 'chat_predecessor_pending'
		)
		RETURNING id
	`, fixture.agentID, chatSessionID, fixture.workspaceID, fixture.userID).Scan(&unresolvedTaskID); err != nil {
		t.Fatalf("create unresolved Pool media Chat tail: %v", err)
	}

	scheduler := &runtimePoolSeamScheduler{}
	bus := events.New()
	seen := make(map[string]string)
	bus.SubscribeAll(func(event events.Event) { seen[event.TaskID] = event.Type })
	svc := &TaskService{Queries: db.New(fixture.tx), Bus: bus, RuntimePool: scheduler}

	if err := svc.PromoteChannelChatTasksIfMediaReady(ctx, chatSessionID); err != nil {
		t.Fatalf("PromoteChannelChatTasksIfMediaReady: %v", err)
	}
	if len(scheduler.assignCalls) != 1 {
		t.Fatalf("allocator calls = %d, want 1 per Workspace", len(scheduler.assignCalls))
	}
	request := scheduler.assignCalls[0]
	if request.WorkspaceID != fixture.workspaceID || request.FocusTaskID.Valid || request.Limit != runtimepool.AssignmentBatchLimit {
		t.Fatalf("allocator request = %+v, want one nonfocused Workspace call", request)
	}
	for _, expected := range []struct {
		taskID     pgtype.UUID
		waitReason string
	}{
		{taskID: fixture.taskID, waitReason: "no_eligible_runtime"},
		{taskID: secondTaskID, waitReason: "no_eligible_runtime"},
		{taskID: unresolvedTaskID, waitReason: "chat_predecessor_pending"},
	} {
		taskID := expected.taskID
		if seen[util.UUIDToString(taskID)] != protocol.EventTaskWaitingRuntime {
			t.Fatalf("Task %s event = %q, want waiting_runtime", util.UUIDToString(taskID), seen[util.UUIDToString(taskID)])
		}
		var status string
		var runtimeID pgtype.UUID
		var waitReason pgtype.Text
		if err := fixture.tx.QueryRow(ctx, `
			SELECT status, runtime_id, wait_reason FROM agent_task_queue WHERE id = $1
		`, taskID).Scan(&status, &runtimeID, &waitReason); err != nil {
			t.Fatalf("load promoted Pool Chat Task %s: %v", util.UUIDToString(taskID), err)
		}
		if status != runtimepool.StatusWaitingRuntime || runtimeID.Valid || !waitReason.Valid || waitReason.String != expected.waitReason {
			t.Fatalf("Pool Chat Task %s = (%q,%v,%v), want waiting_runtime/NULL/%s", util.UUIDToString(taskID), status, runtimeID, waitReason, expected.waitReason)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("events = %v, want exactly three waiting_runtime", seen)
	}
}

func TestWaitingRuntimeQueryMatrix(t *testing.T) {
	sources := map[string]string{}
	for file, path := range map[string]string{
		"agent":              "../../pkg/db/queries/agent.sql",
		"chat":               "../../pkg/db/queries/chat.sql",
		"squad":              "../../pkg/db/queries/squad.sql",
		"autopilot":          "../../pkg/db/queries/autopilot.sql",
		"runtime":            "../../pkg/db/queries/runtime.sql",
		"platform_extension": "../../pkg/db/queries/platform_extension.sql",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s query source: %v", file, err)
		}
		sources[file] = string(raw)
	}

	type queryRule struct {
		file         string
		name         string
		minWaiting   int
		wantContains []string
		forbid       []string
	}
	positive := []queryRule{
		{file: "agent", name: "CancelAgentTasksByIssue", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelAgentTasksByIssueAndAgent", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelAgentTasksByAgent", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelAgentTasksByTriggerComment", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelAgentTasksByChatSession", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelAgentTask", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelQueuedAgentTask", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelQueuedAgentTasksForSession", minWaiting: 2, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelDeferredEscalationsForTask", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "CancelDeferredEscalationsForIssueAgent", minWaiting: 1, wantContains: []string{"session_affinity_state = CASE", "wait_reason = CASE"}},
		{file: "agent", name: "HasActiveTaskForIssue", minWaiting: 1},
		{file: "agent", name: "HasPendingTaskForIssue", minWaiting: 1},
		{file: "agent", name: "HasPendingTaskForIssueAndAgent", minWaiting: 1},
		{file: "agent", name: "HasPendingTaskForIssueAndAgentExcludingTriggerComment", minWaiting: 1},
		{file: "agent", name: "MergeCommentIntoPendingTask", minWaiting: 1},
		{file: "agent", name: "HasActiveTaskForIssueAndAgent", minWaiting: 1},
		{file: "agent", name: "ListActiveTasksByIssue", minWaiting: 1},
		{file: "agent", name: "ListWorkspaceAgentTaskSnapshot", minWaiting: 1},
		{file: "agent", name: "IsPoolChatClaimHead", minWaiting: 1},
		{file: "agent", name: "ListPoolMemberRevocationDependents", minWaiting: 1},
		{file: "agent", name: "ListMemberRevocationLegacyTaskIDs", minWaiting: 1},
		{file: "agent", name: "CancelMemberRevocationTasksByIDs", minWaiting: 1},
		{file: "chat", name: "ListChatMessages", minWaiting: 2},
		{file: "chat", name: "ListChatMessagesForLegacyTask", minWaiting: 2},
		{file: "chat", name: "ReanchorClaimedDirectChatInput", minWaiting: 2},
		{file: "chat", name: "ReanchorNextQueuedDirectChatInput", minWaiting: 2},
		{file: "chat", name: "ListChatMessagesPage", minWaiting: 2},
		{file: "chat", name: "HasActiveChatTaskForSession", minWaiting: 1},
		{file: "chat", name: "HasPendingChatTurnForSession", minWaiting: 1},
		{file: "chat", name: "GetPendingChatTask", minWaiting: 1, forbid: []string{"'deferred'"}},
		{file: "chat", name: "ListPendingChatTasksForSession", minWaiting: 1},
		{file: "chat", name: "ListPendingChatTasksByCreator", minWaiting: 1},
		{file: "chat", name: "HasPendingChatTasksByCreator", minWaiting: 1},
		{file: "chat", name: "IsPoolChatExecutionHead", minWaiting: 1},
		{file: "chat", name: "ListPoolChatMemberRevocationTails", minWaiting: 1},
		{file: "chat", name: "PromoteNextAuthorizedPoolChatTaskAfterMemberRevocation", minWaiting: 2},
		{file: "squad", name: "ListSquadMemberStatusRows", minWaiting: 1},
	}
	negative := []queryRule{
		{file: "agent", name: "CountRunningTasks"},
		{file: "agent", name: "ClaimAgentTask"},
		{file: "agent", name: "GetGlobalEligibleAgentHeadSnapshot"},
		{file: "agent", name: "ClaimAgentTaskForRuntime"},
		{file: "agent", name: "ListQueuedClaimCandidatesByRuntime"},
		{file: "agent", name: "ListQueuedClaimCandidatesByRuntimes"},
		{file: "agent", name: "ListFreshClaimAttemptsByRuntime"},
		{file: "agent", name: "ListFreshClaimAttemptsByRuntimes"},
		{file: "agent", name: "ListStaleClaimCandidatesByRuntime"},
		{file: "agent", name: "ListStaleClaimCandidatesByRuntimes"},
		{file: "agent", name: "GetGlobalEligibleStaleAgentHeadSnapshot"},
		{file: "agent", name: "ReclaimStaleDispatchedTaskForRuntime"},
		{file: "agent", name: "ReclaimStaleDispatchedTasksForRuntimes"},
		{file: "agent", name: "ReclaimStaleDispatchedTaskForAgentRuntime"},
		{file: "agent", name: "CountOtherAgentCapacityForStaleReclaim"},
		{file: "agent", name: "ExtendAgentTaskPrepareLease"},
		{file: "agent", name: "RecoverOrphanedTasksForRuntime"},
		{file: "agent", name: "FailStaleTasks"},
		{file: "agent", name: "ExpireStaleQueuedTasks"},
		{file: "agent", name: "ListPendingTasksByRuntime"},
		{file: "agent", name: "RegisterPlannedCommentForActiveTask"},
		{file: "agent", name: "ListWorkspaceWorkingAgents"},
		{file: "agent", name: "RefreshAgentStatusFromTasks"},
		{file: "agent", name: "PromoteDueDeferredTasksForRuntime"},
		{file: "agent", name: "PromoteDueDeferredTasksForRuntimes"},
		{file: "chat", name: "PrioritizeQueuedChatTask"},
		{file: "runtime", name: "ListPoolRuntimeCandidates"},
		{file: "runtime", name: "CountRuntimeCapacityBearingTasks"},
		{file: "runtime", name: "FailTasksForOfflineRuntimes"},
		{file: "platform_extension", name: "LockIdlePlatformExtensionRuntime"},
		{file: "autopilot", name: "UpdateAutopilotRunRunning"},
		{file: "autopilot", name: "GetAutopilotRunByIssue"},
		{file: "autopilot", name: "FailAutopilotRunsByIssue"},
	}

	for _, rule := range append(positive, negative...) {
		rule := rule
		t.Run(rule.file+"/"+rule.name, func(t *testing.T) {
			query := namedRuntimePoolSQLQuery(t, sources[rule.file], rule.name)
			waitingCount := strings.Count(query, "'waiting_runtime'")
			if rule.minWaiting > 0 && waitingCount < rule.minWaiting {
				t.Fatalf("waiting_runtime literal count = %d, want at least %d\n%s", waitingCount, rule.minWaiting, query)
			}
			if rule.minWaiting == 0 && waitingCount != 0 {
				t.Fatalf("waiting_runtime leaked into fixed/claim/capacity query\n%s", query)
			}
			for _, fragment := range rule.wantContains {
				if !strings.Contains(query, fragment) {
					t.Fatalf("query missing %q\n%s", fragment, query)
				}
			}
			for _, fragment := range rule.forbid {
				if strings.Contains(query, fragment) {
					t.Fatalf("query unexpectedly contains %q\n%s", fragment, query)
				}
			}
		})
	}

	t.Run("autopilot/GetAutopilotTaskByRun", func(t *testing.T) {
		query := namedRuntimePoolSQLQuery(t, sources["autopilot"], "GetAutopilotTaskByRun")
		if strings.Contains(query, "status") || !strings.Contains(query, "WHERE autopilot_run_id = $1") {
			t.Fatalf("Autopilot Task lookup must remain status-agnostic\n%s", query)
		}
	})
}

func TestWaitingRuntimeQueuedCancellationClearsUnresolvedAffinity(t *testing.T) {
	fixture := newRuntimePoolFocusFixture(t, pooltestdb.Open(t))
	ctx := context.Background()
	var chatSessionID pgtype.UUID
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, fixture.workspaceID, fixture.agentID, fixture.userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create cancellation Chat Session: %v", err)
	}
	if _, err := fixture.tx.Exec(ctx, `
		UPDATE agent_task_queue
		SET issue_id = NULL,
		    chat_session_id = $1,
		    session_affinity_state = 'unresolved',
		    session_affinity_runtime_id = NULL,
		    wait_reason = 'chat_predecessor_pending'
		WHERE id = $2
	`, chatSessionID, fixture.taskID); err != nil {
		t.Fatalf("make unresolved waiting_runtime Task: %v", err)
	}

	cancelled, err := db.New(fixture.tx).CancelQueuedAgentTask(ctx, db.CancelQueuedAgentTaskParams{
		ID:            fixture.taskID,
		ChatSessionID: chatSessionID,
	})
	if err != nil {
		t.Fatalf("CancelQueuedAgentTask(waiting_runtime): %v", err)
	}
	if cancelled.Status != "cancelled" ||
		cancelled.SessionAffinityState != runtimepool.SessionAffinityNone ||
		cancelled.SessionAffinityRuntimeID.Valid || cancelled.WaitReason.Valid {
		t.Fatalf("cancelled unresolved Task = status %q affinity (%q,%v) reason %v, want cancelled/none/NULL/NULL",
			cancelled.Status, cancelled.SessionAffinityState, cancelled.SessionAffinityRuntimeID, cancelled.WaitReason)
	}
}

func namedRuntimePoolSQLQuery(t *testing.T, sql, name string) string {
	t.Helper()
	marker := "-- name: " + name
	start := strings.Index(sql, marker)
	if start < 0 {
		t.Fatalf("%s query missing", name)
	}
	query := sql[start:]
	if next := strings.Index(query[1:], "\n-- name:"); next >= 0 {
		query = query[:next+1]
	}
	return query
}

func runtimePoolSeamTask(last byte, workspaceID, status string, assigned bool) db.AgentTaskQueue {
	task := db.AgentTaskQueue{
		ID:      runtimePoolSeamUUID(last),
		AgentID: runtimePoolSeamUUID(last + 40),
		Status:  status,
		Context: []byte(fmt.Sprintf(`{"type":"quick_create","workspace_id":%q}`, workspaceID)),
	}
	if assigned {
		task.RuntimeID = runtimePoolSeamUUID(last + 80)
	}
	return task
}

func runtimePoolSeamUUID(last byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = last
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

type runtimePoolFocusFixture struct {
	tx          pgx.Tx
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	runtimeID   pgtype.UUID
	agentID     pgtype.UUID
	issueID     pgtype.UUID
	taskID      pgtype.UUID
}

func newRuntimePoolFocusFixture(t *testing.T, pool *pgxpool.Pool) *runtimePoolFocusFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin focus fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback focus fixture: %v", err)
		}
	})
	fixture := &runtimePoolFocusFixture{tx: tx}
	seed := time.Now().UnixNano()
	if err := tx.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Runtime Pool Seam', $1) RETURNING id
	`, fmt.Sprintf("runtime-pool-seam-%d@example.test", seed)).Scan(&fixture.userID); err != nil {
		t.Fatalf("create focus user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Runtime Pool Seam', $1, '', 'RPS') RETURNING id
	`, fmt.Sprintf("runtime-pool-seam-%d", seed)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create focus workspace: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("create focus member: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility
		) VALUES ($1, $2, 'local', 'runtime-pool-seam', 'online', '', '{}'::jsonb, $3, 'private')
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Runtime Pool Seam %d", seed), fixture.userID).Scan(&fixture.runtimeID); err != nil {
		t.Fatalf("create focus runtime: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args, runtime_binding_mode, runtime_requirements
		) VALUES ($1, $2, '', 'pool', '{}'::jsonb, NULL, 'private', 'private', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb, 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}'::jsonb)
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Runtime Pool Seam Agent %d", seed), fixture.userID).Scan(&fixture.agentID); err != nil {
		t.Fatalf("create focus agent: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, 'backlog', 'none', 'member', $3, 0, 1) RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Runtime Pool Seam Issue %d", seed), fixture.userID).Scan(&fixture.issueID); err != nil {
		t.Fatalf("create focus issue: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, runtime_binding_mode,
			placement_workspace_id, runtime_requester_user_id, session_affinity_state
		) VALUES ($1, $2, 'waiting_runtime', 0, 'pool', $3, $4, 'none')
		RETURNING id
	`, fixture.agentID, fixture.issueID, fixture.workspaceID, fixture.userID).Scan(&fixture.taskID); err != nil {
		t.Fatalf("create focus task: %v", err)
	}
	return fixture
}

func addQueuedPoolEscalation(t *testing.T, ctx context.Context, fixture *runtimePoolFocusFixture) pgtype.UUID {
	t.Helper()
	var fallbackAgentID pgtype.UUID
	seed := time.Now().UnixNano()
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args, runtime_binding_mode, runtime_requirements
		) VALUES ($1, $2, '', 'pool', '{}'::jsonb, NULL, 'private', 'private', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb, 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}'::jsonb)
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Runtime Pool Fallback %d", seed), fixture.userID).Scan(&fallbackAgentID); err != nil {
		t.Fatalf("create fallback Pool Agent: %v", err)
	}
	var fallbackID pgtype.UUID
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, priority, escalation_for_task_id,
			runtime_binding_mode, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state
		) VALUES ($1, $2, $3, 'queued', 0, $4, 'pool', $5, $6, 'none')
		RETURNING id
	`, fallbackAgentID, fixture.issueID, fixture.runtimeID, fixture.taskID,
		fixture.workspaceID, fixture.userID).Scan(&fallbackID); err != nil {
		t.Fatalf("create queued Pool escalation: %v", err)
	}
	return fallbackID
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
