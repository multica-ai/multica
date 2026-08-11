package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeTargetedClaimFiltersOnlyOuterCAS(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/db/queries/agent.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := namedAgentQuery(t, string(raw), "ClaimAgentTaskForRuntime")
	if got := strings.Count(query, "runtime_id = sqlc.arg(runtime_id)"); got != 1 {
		t.Fatalf("runtime_id predicate count = %d, want exactly one outer UPDATE CAS", got)
	}
	globalHead := query[:strings.Index(query, "UPDATE agent_task_queue")]
	if strings.Contains(globalHead, "runtime_id") {
		t.Fatal("global eligible head must not be filtered by Runtime")
	}
}

func namedAgentQuery(t *testing.T, sql, name string) string {
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

func TestRuntimeTargetedClaimSingularTwoRuntimes(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 5)
	lowerA := fixture.addIssueTask(t, fixture.runtimeA, "queued", 1)
	headB := fixture.addIssueTask(t, fixture.runtimeB, "queued", 10)

	claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
	if err != nil {
		t.Fatalf("claim Runtime A: %v", err)
	}
	if claimed != nil {
		t.Fatalf("Runtime A claimed %s, want nil while global head belongs to Runtime B", util.UUIDToString(claimed.ID))
	}
	fixture.requireTaskStatus(t, lowerA, "queued")
	fixture.requireTaskStatus(t, headB, "queued")

	claimed, err = fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeB)
	if err != nil {
		t.Fatalf("claim Runtime B: %v", err)
	}
	if claimed == nil || claimed.ID != headB {
		t.Fatalf("Runtime B claim = %+v, want global head %s", claimed, util.UUIDToString(headB))
	}
	fixture.requireTaskStatus(t, lowerA, "queued")
	fixture.requireTaskStatus(t, headB, "dispatched")
}

func TestRuntimeTargetedStaleReclaimTwoRuntimes(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 5)
	lowerA := fixture.addIssueTask(t, fixture.runtimeA, "dispatched", 1)
	headB := fixture.addIssueTask(t, fixture.runtimeB, "dispatched", 10)

	claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
	if err != nil {
		t.Fatalf("reclaim Runtime A: %v", err)
	}
	if claimed != nil {
		t.Fatalf("Runtime A reclaimed %s, want nil while global stale head belongs to Runtime B", util.UUIDToString(claimed.ID))
	}
	fixture.requireStaleGeneration(t, lowerA)
	fixture.requireStaleGeneration(t, headB)

	claimed, err = fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeB)
	if err != nil {
		t.Fatalf("reclaim Runtime B: %v", err)
	}
	if claimed == nil || claimed.ID != headB {
		t.Fatalf("Runtime B reclaim = %+v, want global stale head %s", claimed, util.UUIDToString(headB))
	}
	if !claimed.PrepareLeaseExpiresAt.Valid || !claimed.PrepareLeaseExpiresAt.Time.After(time.Now()) {
		t.Fatalf("reclaimed prepare lease = %+v, want refreshed future lease", claimed.PrepareLeaseExpiresAt)
	}
	fixture.requireStaleGeneration(t, lowerA)
}

func TestRuntimeTargetedStaleReclaimMaxOneExcludesItself(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 1)
	stale := fixture.addIssueTask(t, fixture.runtimeA, "dispatched", 1)

	claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
	if err != nil {
		t.Fatalf("reclaim max=1 stale Task: %v", err)
	}
	if claimed == nil || claimed.ID != stale {
		t.Fatalf("reclaim = %+v, want stale Task %s", claimed, util.UUIDToString(stale))
	}
}

func TestRuntimeTargetedFreshInvalidRequeues(t *testing.T) {
	tests := []struct {
		name       string
		pinned     bool
		mutate     func(*testing.T, *runtimeTargetedClaimFixture)
		wantReason string
	}{
		{
			name: "none capability downgrade",
			mutate: func(t *testing.T, f *runtimeTargetedClaimFixture) {
				f.exec(t, `UPDATE agent_runtime SET capabilities = '{}'::text[] WHERE id = $1`, f.runtimeA)
			},
			wantReason: "no_eligible_runtime",
		},
		{
			name:   "pinned capability downgrade",
			pinned: true,
			mutate: func(t *testing.T, f *runtimeTargetedClaimFixture) {
				f.exec(t, `UPDATE agent_runtime SET capabilities = '{}'::text[] WHERE id = $1`, f.runtimeA)
			},
			wantReason: "session_runtime_capability_mismatch",
		},
		{
			name:   "pinned access revoked",
			pinned: true,
			mutate: func(t *testing.T, f *runtimeTargetedClaimFixture) {
				f.exec(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.workspaceID, f.userID)
			},
			wantReason: "session_runtime_unauthorized",
		},
		{
			name:   "pinned Runtime offline",
			pinned: true,
			mutate: func(t *testing.T, f *runtimeTargetedClaimFixture) {
				f.exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, f.runtimeA)
			},
			wantReason: "session_runtime_offline",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeTargetedClaimFixture(t, 5)
			taskID := fixture.addIssueTask(t, fixture.runtimeA, "queued", 1)
			if test.pinned {
				fixture.exec(t, `
					UPDATE agent_task_queue
					SET session_affinity_state = 'pinned', session_affinity_runtime_id = $1
					WHERE id = $2
				`, fixture.runtimeA, taskID)
			}
			test.mutate(t, fixture)

			claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
			if err != nil {
				t.Fatalf("claim invalid fresh Task: %v", err)
			}
			if claimed != nil {
				t.Fatalf("invalid fresh Task claimed: %+v", claimed)
			}
			var status, reason string
			var runtimeID pgtype.UUID
			if err := fixture.pool.QueryRow(context.Background(), `
				SELECT status, runtime_id, wait_reason
				FROM agent_task_queue WHERE id = $1
			`, taskID).Scan(&status, &runtimeID, &reason); err != nil {
				t.Fatalf("read requeued Task: %v", err)
			}
			if status != runtimepool.StatusWaitingRuntime || runtimeID.Valid || reason != test.wantReason {
				t.Fatalf("requeued Task = status=%s runtime=%s reason=%s, want waiting/null/%s",
					status, util.UUIDToString(runtimeID), reason, test.wantReason)
			}
		})
	}
}

func TestRuntimeTargetedStaleInvalidUsesRecoveryCancel(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 1)
	stale := fixture.addIssueTask(t, fixture.runtimeA, "dispatched", 1)
	fixture.exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, fixture.runtimeA)

	var eventTypes []string
	fixture.service.Bus.SubscribeAll(func(event events.Event) {
		if event.TaskID == util.UUIDToString(stale) {
			eventTypes = append(eventTypes, event.Type)
		}
	})
	claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
	if err != nil {
		t.Fatalf("reclaim invalid stale Task: %v", err)
	}
	if claimed != nil {
		t.Fatalf("invalid stale Task was re-delivered: %+v", claimed)
	}
	fixture.requireTaskStatus(t, stale, "cancelled")
	if got := strings.Join(eventTypes, ","); got != "task:cancelled" {
		t.Fatalf("invalid stale events = %q, want task:cancelled only", got)
	}
}

func TestRuntimeTargetedClaimBatchOrdersResolvedGlobalHeads(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 5)
	agentB := fixture.addPoolAgent(t, "Runtime Targeted Agent B", 5)
	ctx := context.Background()

	// Agent A's highest raw Runtime candidate is blocked by another active
	// Task on the same Issue. Its actual eligible head is low priority.
	var blockedIssue pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type,
			number, position
		)
		VALUES ($1, 'blocked raw candidate', 'in_progress', 'none', $2, 'member', 919991, 1)
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&blockedIssue); err != nil {
		t.Fatalf("create blocked Issue: %v", err)
	}
	var blockedHigh pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, context, runtime_id,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		VALUES ($1, $2, 'queued', 100, '{}'::jsonb, $3, 'pool', $4::jsonb, $5, $6, $6, 'none')
		RETURNING id
	`, fixture.agentID, blockedIssue, fixture.runtimeA, runtimeTargetedRequirementsJSON(), fixture.workspaceID, fixture.userID).Scan(&blockedHigh); err != nil {
		t.Fatalf("create blocked high Task: %v", err)
	}
	fixture.exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, context, runtime_id, dispatched_at, started_at,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		VALUES ($1, $2, 'running', 0, '{}'::jsonb, $3, now(), now(),
			'pool', $4::jsonb, $5, $6, $6, 'none')
	`, fixture.agentID, blockedIssue, fixture.runtimeA, runtimeTargetedRequirementsJSON(), fixture.workspaceID, fixture.userID)

	lowA := fixture.addIssueTaskForAgent(t, fixture.agentID, fixture.runtimeA, "queued", 1)
	mediumB := fixture.addIssueTaskForAgent(t, agentB, fixture.runtimeB, "queued", 50)
	claimed, err := fixture.service.ClaimTasksForRuntimes(ctx, []pgtype.UUID{fixture.runtimeA, fixture.runtimeB}, 1)
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != mediumB {
		t.Fatalf("batch claimed %+v, want medium real head %s before low Agent-A head",
			claimed, util.UUIDToString(mediumB))
	}
	fixture.requireTaskStatus(t, blockedHigh, "queued")
	fixture.requireTaskStatus(t, lowA, "queued")
}

func TestRuntimeTargetedClaimBatchDedupesAgentAcrossStaleAndFresh(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 5)
	stale := fixture.addIssueTask(t, fixture.runtimeA, "dispatched", 10)
	fresh := fixture.addIssueTask(t, fixture.runtimeB, "queued", 5)

	claimed, err := fixture.service.ClaimTasksForRuntimes(context.Background(), []pgtype.UUID{fixture.runtimeA, fixture.runtimeB}, 2)
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != stale {
		t.Fatalf("batch claimed %+v, want only stale Task %s for this Agent", claimed, util.UUIDToString(stale))
	}
	fixture.requireTaskStatus(t, fresh, "queued")
}

func TestRuntimeTargetedClaimPreservesDirectChatReanchor(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantMoved bool
	}{
		{name: "fresh claim reanchors", status: "queued", wantMoved: true},
		{name: "stale reclaim never reanchors", status: "dispatched", wantMoved: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeTargetedClaimFixture(t, 1)
			taskID, inputID, before := fixture.addDirectChatTask(t, test.status)
			claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
			if err != nil {
				t.Fatalf("claim direct Chat Task: %v", err)
			}
			if claimed == nil || claimed.ID != taskID {
				t.Fatalf("claimed = %+v, want Chat Task %s", claimed, util.UUIDToString(taskID))
			}
			var after time.Time
			if err := fixture.pool.QueryRow(context.Background(), `SELECT created_at FROM chat_message WHERE id = $1`, inputID).Scan(&after); err != nil {
				t.Fatalf("read Chat input timestamp: %v", err)
			}
			if test.wantMoved && !after.After(before) {
				t.Fatalf("fresh Chat input timestamp did not move: before=%s after=%s", before, after)
			}
			if !test.wantMoved && !after.Equal(before) {
				t.Fatalf("stale re-delivery moved Chat input: before=%s after=%s", before, after)
			}
		})
	}
}

func TestRuntimeTargetedStalePreviewDistinctAttemptsDoNotStarve(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 256)
	agentB := fixture.addPoolAgent(t, "eligible stale Agent", 1)
	runtimeOutside := fixture.addRuntime(t, "outside requested Runtime")
	ctx := context.Background()

	// 129 stale rows on a requested Runtime all belong to Agent A, but A's
	// real global stale head is on an unrequested Runtime. A raw-row LIMIT 128
	// would repeat this prefix forever and hide Agent B.
	fixture.exec(t, `
		WITH created_issues AS (
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_id, creator_type,
				number, position
			)
			SELECT $1, 'stale duplicate ' || n, 'in_progress', 'none', $2, 'member',
				930000 + n, n
			FROM generate_series(1, 129) AS n
			RETURNING id, number
		)
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, context, runtime_id, dispatched_at,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		SELECT $3, id, 'dispatched', number - 930000, '{}'::jsonb, $4,
			now() - interval '2 minutes', 'pool', $5::jsonb, $1, $2, $2, 'none'
		FROM created_issues
	`, fixture.workspaceID, fixture.userID, fixture.agentID, fixture.runtimeA, runtimeTargetedRequirementsJSON())
	fixture.addIssueTaskForAgent(t, fixture.agentID, runtimeOutside, "dispatched", 10000)
	eligibleB := fixture.addIssueTaskForAgent(t, agentB, fixture.runtimeB, "dispatched", 1)

	claimed, err := fixture.service.ClaimTasksForRuntimes(ctx, []pgtype.UUID{fixture.runtimeA, fixture.runtimeB}, 1)
	if err != nil {
		t.Fatalf("batch stale claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != eligibleB {
		t.Fatalf("batch stale claim = %+v, want non-starved Agent-B Task %s", claimed, util.UUIDToString(eligibleB))
	}
}

func TestRuntimeTargetedClaimDowngradeRace(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(context.Context, *testing.T, *runtimeTargetedClaimFixture, interface {
			Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		})
		wantReason string
	}{
		{
			name: "capability downgrade waits for Runtime lock",
			mutate: func(ctx context.Context, t *testing.T, f *runtimeTargetedClaimFixture, tx interface {
				Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
			}) {
				if _, err := tx.Exec(ctx, `UPDATE agent_runtime SET capabilities = '{}'::text[] WHERE id = $1`, f.runtimeA); err != nil {
					t.Fatalf("hold capability downgrade: %v", err)
				}
			},
			wantReason: "session_runtime_capability_mismatch",
		},
		{
			name: "access revoke waits for Member lock",
			mutate: func(ctx context.Context, t *testing.T, f *runtimeTargetedClaimFixture, tx interface {
				Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
			}) {
				if _, err := tx.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.workspaceID, f.userID); err != nil {
					t.Fatalf("hold Member revoke: %v", err)
				}
			},
			wantReason: "session_runtime_unauthorized",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeTargetedClaimFixture(t, 1)
			taskID := fixture.addIssueTask(t, fixture.runtimeA, "queued", 1)
			fixture.exec(t, `
				UPDATE agent_task_queue
				SET session_affinity_state = 'pinned', session_affinity_runtime_id = $1
				WHERE id = $2
			`, fixture.runtimeA, taskID)

			ctx := context.Background()
			tx, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin downgrade: %v", err)
			}
			defer tx.Rollback(ctx)
			test.mutate(ctx, t, fixture, tx)

			done := make(chan runtimeTargetedClaimResult, 1)
			go func() {
				task, claimErr := fixture.service.ClaimTaskForRuntime(ctx, fixture.runtimeA)
				done <- runtimeTargetedClaimResult{task: task, err: claimErr}
			}()
			select {
			case result := <-done:
				t.Fatalf("claim escaped locked downgrade before commit: task=%+v err=%v", result.task, result.err)
			case <-time.After(100 * time.Millisecond):
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit downgrade: %v", err)
			}
			select {
			case result := <-done:
				if result.err != nil || result.task != nil {
					t.Fatalf("claim after downgrade = task=%+v err=%v", result.task, result.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("claim did not resume after downgrade commit")
			}
			var status, reason string
			if err := fixture.pool.QueryRow(ctx, `SELECT status, wait_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &reason); err != nil {
				t.Fatalf("read downgraded Task: %v", err)
			}
			if status != runtimepool.StatusWaitingRuntime || reason != test.wantReason {
				t.Fatalf("downgraded Task = %s/%s, want waiting/%s", status, reason, test.wantReason)
			}
		})
	}
}

func TestRuntimeTargetedStaleLeaseExtensionWinsInvalidCancel(t *testing.T) {
	fixture := newRuntimeTargetedClaimFixture(t, 1)
	stale := fixture.addIssueTask(t, fixture.runtimeA, "dispatched", 1)
	fixture.exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, fixture.runtimeA)
	ctx := context.Background()
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lease extension: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE agent_task_queue
		SET prepare_lease_expires_at = now() + interval '5 minutes'
		WHERE id = $1
	`, stale); err != nil {
		t.Fatalf("hold lease extension: %v", err)
	}
	done := make(chan runtimeTargetedClaimResult, 1)
	go func() {
		task, claimErr := fixture.service.ClaimTaskForRuntime(ctx, fixture.runtimeA)
		done <- runtimeTargetedClaimResult{task: task, err: claimErr}
	}()
	select {
	case result := <-done:
		t.Fatalf("invalid stale cancel escaped locked lease before commit: task=%+v err=%v", result.task, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lease extension: %v", err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.task != nil {
			t.Fatalf("claim after lease extension = task=%+v err=%v", result.task, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not resume after lease commit")
	}
	var status string
	var lease time.Time
	if err := fixture.pool.QueryRow(ctx, `SELECT status, prepare_lease_expires_at FROM agent_task_queue WHERE id = $1`, stale).Scan(&status, &lease); err != nil {
		t.Fatalf("read lease-protected Task: %v", err)
	}
	if status != "dispatched" || !lease.After(time.Now()) {
		t.Fatalf("lease-protected stale Task = status=%s lease=%s, want dispatched/future", status, lease)
	}
}

func TestRuntimeTargetedClaimRevalidatesLockedHeadSnapshot(t *testing.T) {
	t.Run("routing snapshot drift", func(t *testing.T) {
		fixture := newRuntimeTargetedClaimFixture(t, 1)
		head := fixture.addIssueTask(t, fixture.runtimeA, "queued", 10)
		ctx := context.Background()
		tx, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin routing drift: %v", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2 FOR UPDATE`, fixture.workspaceID, fixture.userID); err != nil {
			t.Fatalf("lock Member: %v", err)
		}
		done := make(chan runtimeTargetedClaimResult, 1)
		go func() {
			task, claimErr := fixture.service.ClaimTaskForRuntime(ctx, fixture.runtimeA)
			done <- runtimeTargetedClaimResult{task: task, err: claimErr}
		}()
		select {
		case result := <-done:
			t.Fatalf("claim escaped Member lock before snapshot drift: task=%+v err=%v", result.task, result.err)
		case <-time.After(100 * time.Millisecond):
		}
		if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET explicit_fresh_session = true WHERE id = $1`, head); err != nil {
			t.Fatalf("drift routing snapshot: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit routing drift: %v", err)
		}
		result := <-done
		if result.err != nil || result.task != nil {
			t.Fatalf("claim after routing drift = task=%+v err=%v", result.task, result.err)
		}
		fixture.requireTaskStatus(t, head, "queued")
	})

	t.Run("locked head never skips lower exact snapshot", func(t *testing.T) {
		fixture := newRuntimeTargetedClaimFixture(t, 2)
		head := fixture.addIssueTask(t, fixture.runtimeA, "queued", 10)
		lower := fixture.addIssueTask(t, fixture.runtimeA, "queued", 1)
		ctx := context.Background()
		tx, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin head lock: %v", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT id FROM agent_task_queue WHERE id = $1 FOR UPDATE`, head); err != nil {
			t.Fatalf("lock global head: %v", err)
		}
		claimed, err := fixture.service.ClaimTaskForRuntime(ctx, fixture.runtimeA)
		if err != nil {
			t.Fatalf("claim with locked head: %v", err)
		}
		if claimed != nil {
			t.Fatalf("claim skipped locked head to lower Task: %+v", claimed)
		}
		fixture.requireTaskStatus(t, lower, "queued")
	})
}

func TestClaimSerializationAcrossRuntimes(t *testing.T) {
	tests := []struct {
		name          string
		maxConcurrent int32
		setup         func(*testing.T, *runtimeTargetedClaimFixture) pgtype.UUID
	}{
		{
			name:          "Agent capacity",
			maxConcurrent: 1,
			setup: func(t *testing.T, f *runtimeTargetedClaimFixture) pgtype.UUID {
				activeIssue := f.addIssue(t, "active capacity Issue")
				queuedIssue := f.addIssue(t, "queued capacity Issue")
				f.addLinkedPoolTask(t, f.agentID, f.runtimeA, "running", 0, activeIssue, pgtype.UUID{})
				return f.addLinkedPoolTask(t, f.agentID, f.runtimeB, "queued", 10, queuedIssue, pgtype.UUID{})
			},
		},
		{
			name:          "Issue serialization",
			maxConcurrent: 5,
			setup: func(t *testing.T, f *runtimeTargetedClaimFixture) pgtype.UUID {
				issueID := f.addIssue(t, "shared serialized Issue")
				f.addLinkedPoolTask(t, f.agentID, f.runtimeA, "running", 0, issueID, pgtype.UUID{})
				return f.addLinkedPoolTask(t, f.agentID, f.runtimeB, "queued", 10, issueID, pgtype.UUID{})
			},
		},
		{
			name:          "Chat serialization",
			maxConcurrent: 5,
			setup: func(t *testing.T, f *runtimeTargetedClaimFixture) pgtype.UUID {
				sessionID := f.addChatSession(t)
				f.addLinkedPoolTask(t, f.agentID, f.runtimeA, "running", 0, pgtype.UUID{}, sessionID)
				return f.addLinkedPoolTask(t, f.agentID, f.runtimeB, "queued", 10, pgtype.UUID{}, sessionID)
			},
		},
		{
			name:          "Quick serialization",
			maxConcurrent: 5,
			setup: func(t *testing.T, f *runtimeTargetedClaimFixture) pgtype.UUID {
				f.addLinkedPoolTask(t, f.agentID, f.runtimeA, "running", 0, pgtype.UUID{}, pgtype.UUID{})
				return f.addLinkedPoolTask(t, f.agentID, f.runtimeB, "queued", 10, pgtype.UUID{}, pgtype.UUID{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeTargetedClaimFixture(t, test.maxConcurrent)
			queued := test.setup(t, fixture)
			claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeB)
			if err != nil {
				t.Fatalf("claim blocked cross-Runtime Task: %v", err)
			}
			if claimed != nil {
				t.Fatalf("cross-Runtime barrier was bypassed: %+v", claimed)
			}
			fixture.requireTaskStatus(t, queued, "queued")
		})
	}
}

func TestRuntimeTargetedFixedClaimPreservesLegacyBehavior(t *testing.T) {
	t.Run("wrong Runtime cannot skip fixed global head", func(t *testing.T) {
		fixture := newRuntimeTargetedClaimFixture(t, 1)
		agentID := fixture.addFixedAgent(t, "fixed wrong Runtime", fixture.runtimeA, 1)
		headB := fixture.addFixedIssueTask(t, agentID, fixture.runtimeB, 10)
		claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
		if err != nil {
			t.Fatalf("claim wrong fixed Runtime: %v", err)
		}
		if claimed != nil {
			t.Fatalf("wrong fixed Runtime claimed global head: %+v", claimed)
		}
		fixture.requireTaskStatus(t, headB, "queued")
		claimed, err = fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeB)
		if err != nil || claimed == nil || claimed.ID != headB {
			t.Fatalf("right fixed Runtime claim = task=%+v err=%v, want %s", claimed, err, util.UUIDToString(headB))
		}
	})

	t.Run("fixed Agent rebind does not move stamped Task", func(t *testing.T) {
		fixture := newRuntimeTargetedClaimFixture(t, 1)
		agentID := fixture.addFixedAgent(t, "fixed rebound", fixture.runtimeA, 1)
		stampedA := fixture.addFixedIssueTask(t, agentID, fixture.runtimeA, 1)
		fixture.exec(t, `UPDATE agent SET runtime_id = $1 WHERE id = $2`, fixture.runtimeB, agentID)
		claimed, err := fixture.service.ClaimTaskForRuntime(context.Background(), fixture.runtimeA)
		if err != nil || claimed == nil || claimed.ID != stampedA {
			t.Fatalf("rebound fixed claim = task=%+v err=%v, want stamped Runtime-A Task %s",
				claimed, err, util.UUIDToString(stampedA))
		}
	})

	t.Run("non Runtime ClaimTask remains Agent global", func(t *testing.T) {
		fixture := newRuntimeTargetedClaimFixture(t, 1)
		agentID := fixture.addFixedAgent(t, "legacy fixed claim", fixture.runtimeA, 1)
		headB := fixture.addFixedIssueTask(t, agentID, fixture.runtimeB, 10)
		claimed, err := fixture.service.ClaimTask(context.Background(), agentID)
		if err != nil || claimed == nil || claimed.ID != headB {
			t.Fatalf("legacy ClaimTask = task=%+v err=%v, want Agent global head %s",
				claimed, err, util.UUIDToString(headB))
		}
	})
}

type runtimeTargetedClaimResult struct {
	task *db.AgentTaskQueue
	err  error
}

type runtimeTargetedClaimFixture struct {
	pool        *pgxpool.Pool
	service     *TaskService
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	agentID     pgtype.UUID
	runtimeA    pgtype.UUID
	runtimeB    pgtype.UUID
	issueNumber int
}

func newRuntimeTargetedClaimFixture(t *testing.T, maxConcurrent int32) *runtimeTargetedClaimFixture {
	t.Helper()
	ctx := context.Background()
	pool := pooltestdb.Open(t)
	fixture := &runtimeTargetedClaimFixture{pool: pool}
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime Targeted Claim', $1)
		RETURNING id
	`, fmt.Sprintf("runtime-targeted-claim-%d@multica.test", suffix)).Scan(&fixture.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Runtime Targeted Claim', $1, '', 'RTC')
		RETURNING id
	`, fmt.Sprintf("runtime-targeted-claim-%d", suffix)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create Workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("create Member: %v", err)
	}

	addRuntime := func(name string) pgtype.UUID {
		var runtimeID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, name, runtime_mode, provider, status, device_info,
				metadata, last_seen_at, owner_id, visibility, capabilities
			)
			VALUES ($1, $2, 'local', 'runtime-targeted-test', 'online', 'test',
				'{}'::jsonb, now(), $3, 'private', $4::text[])
			RETURNING id
		`, fixture.workspaceID, name, fixture.userID, []string{runtimepool.CapabilityExtensionExecuteV1}).Scan(&runtimeID); err != nil {
			t.Fatalf("create Runtime %s: %v", name, err)
		}
		return runtimeID
	}
	fixture.runtimeA = addRuntime("Runtime A")
	fixture.runtimeB = addRuntime("Runtime B")

	requirements := runtimeTargetedRequirementsJSON()
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, max_concurrent_tasks, owner_id, runtime_binding_mode,
			runtime_requirements
		)
		VALUES ($1, 'Runtime Targeted Agent', '', 'pool', '{}'::jsonb,
			'private', $2, $3, 'pool', $4::jsonb)
		RETURNING id
	`, fixture.workspaceID, maxConcurrent, fixture.userID, requirements).Scan(&fixture.agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	fixture.service = NewTaskService(db.New(pool), pool, nil, events.New())

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM issue WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM chat_session WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fixture.workspaceID, fixture.userID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, fixture.userID)
	})
	return fixture
}

func (f *runtimeTargetedClaimFixture) addIssueTask(t *testing.T, runtimeID pgtype.UUID, status string, priority int32) pgtype.UUID {
	t.Helper()
	return f.addIssueTaskForAgent(t, f.agentID, runtimeID, status, priority)
}

func (f *runtimeTargetedClaimFixture) addIssueTaskForAgent(t *testing.T, agentID, runtimeID pgtype.UUID, status string, priority int32) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	f.issueNumber++
	var issueID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type,
			number, position
		)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, $5)
		RETURNING id
	`, f.workspaceID, fmt.Sprintf("Runtime target issue %d", f.issueNumber), f.userID, 910000+f.issueNumber, f.issueNumber).Scan(&issueID); err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	requirements := runtimeTargetedRequirementsJSON()
	var taskID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, context, runtime_id,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5, 'pool', $6::jsonb, $7, $8, $8, 'none')
		RETURNING id
	`, agentID, issueID, status, priority, runtimeID, requirements, f.workspaceID, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("create Task: %v", err)
	}
	if status == "dispatched" {
		if _, err := f.pool.Exec(ctx, `
			UPDATE agent_task_queue
			SET dispatched_at = now() - interval '2 minutes',
				prepare_lease_expires_at = NULL
			WHERE id = $1
		`, taskID); err != nil {
			t.Fatalf("make Task stale: %v", err)
		}
	}
	return taskID
}

func (f *runtimeTargetedClaimFixture) addPoolAgent(t *testing.T, name string, maxConcurrent int32) pgtype.UUID {
	t.Helper()
	var agentID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, max_concurrent_tasks, owner_id, runtime_binding_mode,
			runtime_requirements
		)
		VALUES ($1, $2, '', 'pool', '{}'::jsonb,
			'private', $3, $4, 'pool', $5::jsonb)
		RETURNING id
	`, f.workspaceID, name, maxConcurrent, f.userID, runtimeTargetedRequirementsJSON()).Scan(&agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	return agentID
}

func (f *runtimeTargetedClaimFixture) addFixedAgent(t *testing.T, name string, runtimeID pgtype.UUID, maxConcurrent int32) pgtype.UUID {
	t.Helper()
	var agentID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, max_concurrent_tasks, owner_id, runtime_binding_mode
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3,
			'private', $4, $5, 'fixed')
		RETURNING id
	`, f.workspaceID, name, runtimeID, maxConcurrent, f.userID).Scan(&agentID); err != nil {
		t.Fatalf("create fixed Agent: %v", err)
	}
	return agentID
}

func (f *runtimeTargetedClaimFixture) addFixedIssueTask(t *testing.T, agentID, runtimeID pgtype.UUID, priority int32) pgtype.UUID {
	t.Helper()
	issueID := f.addIssue(t, "fixed Runtime-targeted Issue")
	var taskID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, priority, context, runtime_id,
			runtime_binding_mode, runtime_requirements, session_affinity_state
		)
		VALUES ($1, $2, 'queued', $3, '{}'::jsonb, $4,
			'fixed', '{}'::jsonb, 'none')
		RETURNING id
	`, agentID, issueID, priority, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create fixed Task: %v", err)
	}
	return taskID
}

func (f *runtimeTargetedClaimFixture) addRuntime(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	var runtimeID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info,
			metadata, last_seen_at, owner_id, visibility, capabilities
		)
		VALUES ($1, $2, 'local', 'runtime-targeted-test', 'online', 'test',
			'{}'::jsonb, now(), $3, 'private', $4::text[])
		RETURNING id
	`, f.workspaceID, name, f.userID, []string{runtimepool.CapabilityExtensionExecuteV1}).Scan(&runtimeID); err != nil {
		t.Fatalf("create Runtime: %v", err)
	}
	return runtimeID
}

func (f *runtimeTargetedClaimFixture) addDirectChatTask(t *testing.T, status string) (pgtype.UUID, pgtype.UUID, time.Time) {
	t.Helper()
	ctx := context.Background()
	var sessionID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Runtime targeted direct Chat')
		RETURNING id
	`, f.workspaceID, f.agentID, f.userID).Scan(&sessionID); err != nil {
		t.Fatalf("create Chat Session: %v", err)
	}
	var taskID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, status, priority, context, runtime_id,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		VALUES ($1, $2, $3, 1, '{}'::jsonb, $4, 'pool', $5::jsonb, $6, $7, $7, 'none')
		RETURNING id
	`, f.agentID, sessionID, status, f.runtimeA, runtimeTargetedRequirementsJSON(), f.workspaceID, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("create direct Chat Task: %v", err)
	}
	f.exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, taskID)
	if status == "dispatched" {
		f.exec(t, `
			UPDATE agent_task_queue
			SET dispatched_at = now() - interval '2 minutes', prepare_lease_expires_at = NULL
			WHERE id = $1
		`, taskID)
	}
	before := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Microsecond)
	var inputID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id, created_at)
		VALUES ($1, 'user', 'move me', $2, $3)
		RETURNING id
	`, sessionID, taskID, before).Scan(&inputID); err != nil {
		t.Fatalf("create direct Chat input: %v", err)
	}
	f.exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content, created_at)
		VALUES ($1, 'assistant', 'previous outcome', now() - interval '1 minute')
	`, sessionID)
	return taskID, inputID, before
}

func (f *runtimeTargetedClaimFixture) addIssue(t *testing.T, title string) pgtype.UUID {
	t.Helper()
	f.issueNumber++
	var issueID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type,
			number, position
		)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, $5)
		RETURNING id
	`, f.workspaceID, title, f.userID, 940000+f.issueNumber, f.issueNumber).Scan(&issueID); err != nil {
		t.Fatalf("create linked Issue: %v", err)
	}
	return issueID
}

func (f *runtimeTargetedClaimFixture) addChatSession(t *testing.T) pgtype.UUID {
	t.Helper()
	var sessionID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Runtime targeted serialization')
		RETURNING id
	`, f.workspaceID, f.agentID, f.userID).Scan(&sessionID); err != nil {
		t.Fatalf("create serialized Chat Session: %v", err)
	}
	return sessionID
}

func (f *runtimeTargetedClaimFixture) addLinkedPoolTask(
	t *testing.T,
	agentID, runtimeID pgtype.UUID,
	status string,
	priority int32,
	issueID, chatSessionID pgtype.UUID,
) pgtype.UUID {
	t.Helper()
	var taskID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, chat_session_id, status, priority, context, runtime_id,
			dispatched_at, started_at, runtime_binding_mode, runtime_requirements,
			placement_workspace_id, runtime_requester_user_id, runtime_trigger_user_id, session_affinity_state
		)
		VALUES (
			$1, $2, $3, $4::text, $5, '{}'::jsonb, $6,
			CASE WHEN $4::text IN ('dispatched', 'running') THEN now() END,
			CASE WHEN $4::text = 'running' THEN now() END,
			'pool', $7::jsonb, $8, $9, $9, 'none'
		)
		RETURNING id
	`, agentID, issueID, chatSessionID, status, priority, runtimeID,
		runtimeTargetedRequirementsJSON(), f.workspaceID, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("create linked Pool Task: %v", err)
	}
	return taskID
}

func runtimeTargetedRequirementsJSON() string {
	return fmt.Sprintf(`{"schema_version":"%s","capabilities_all":["%s"]}`,
		runtimepool.RequirementsSchemaV1, runtimepool.CapabilityExtensionExecuteV1)
}

func (f *runtimeTargetedClaimFixture) requireTaskStatus(t *testing.T, taskID pgtype.UUID, want string) {
	t.Helper()
	var got string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&got); err != nil {
		t.Fatalf("read Task status: %v", err)
	}
	if got != want {
		t.Fatalf("Task %s status = %s, want %s", util.UUIDToString(taskID), got, want)
	}
}

func (f *runtimeTargetedClaimFixture) requireStaleGeneration(t *testing.T, taskID pgtype.UUID) {
	t.Helper()
	var dispatchedAt time.Time
	var lease pgtype.Timestamptz
	if err := f.pool.QueryRow(context.Background(), `
		SELECT dispatched_at, prepare_lease_expires_at
		FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&dispatchedAt, &lease); err != nil {
		t.Fatalf("read stale Task generation: %v", err)
	}
	if !dispatchedAt.Before(time.Now().Add(-claimResponseRecoveryWindow)) || lease.Valid {
		t.Fatalf("Task %s no longer stale: dispatched_at=%s lease=%+v",
			util.UUIDToString(taskID), dispatchedAt, lease)
	}
}

func (f *runtimeTargetedClaimFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture mutation: %v", err)
	}
}
