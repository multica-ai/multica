package service

import (
	"context"
	"errors"
	"fmt"
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

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
