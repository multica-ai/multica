package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCreateRetryTaskFireAtControlsDeferral locks in the SQL half of the
// three-tier provider_network schedule (MUL-4910): CreateRetryTask inserts a
// 'deferred' child carrying fire_at when the fire_at param is set (the final,
// backed-off attempt) and an immediately-claimable 'queued' child when it is
// NULL (every other retry). Both continue the resume chain — force_fresh_session
// stays false for a provider_network parent.
func TestCreateRetryTaskFireAtControlsDeferral(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	// agent_task_queue.runtime_id is NOT NULL; reuse the fixture agent's runtime.
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	// Parent: a provider_network failure on its second attempt — the point at
	// which the schedule wants the next (final) retry deferred.
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, failure_reason, session_id, work_dir, channel_context_revision)
		VALUES ($1, $2, $3, 'failed', 0, 2, 2, 'agent_error.provider_network', 'src-session', '/tmp/src-workdir', 7)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	cases := []struct {
		name            string
		fireAt          pgtype.Timestamptz
		maxAttempts     pgtype.Int4
		wantStatus      string
		wantFireAt      bool
		wantMaxAttempts int32
	}{
		// Final tier: deferred, and the effective budget (3) written into the row
		// so it self-describes as attempt=3/max_attempts=3, not attempt=3/max=2.
		{"deferred final tier persists budget", pgtype.Timestamptz{Time: time.Now().Add(5 * time.Second), Valid: true}, pgtype.Int4{Int32: 3, Valid: true}, "deferred", true, 3},
		// NULL max_attempts inherits the parent's column (COALESCE fallback).
		{"queued immediate tier inherits budget", pgtype.Timestamptz{}, pgtype.Int4{}, "queued", false, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			child, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID, FireAt: tc.fireAt, MaxAttempts: tc.maxAttempts})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, child.ID) })

			if child.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", child.Status, tc.wantStatus)
			}
			if child.FireAt.Valid != tc.wantFireAt {
				t.Errorf("fire_at valid = %v, want %v", child.FireAt.Valid, tc.wantFireAt)
			}
			if child.Attempt != 3 {
				t.Errorf("attempt = %d, want 3 (parent attempt 2 + 1)", child.Attempt)
			}
			if child.MaxAttempts != tc.wantMaxAttempts {
				t.Errorf("max_attempts = %d, want %d", child.MaxAttempts, tc.wantMaxAttempts)
			}
			// provider_network is resume-safe: the retry must continue the session.
			if child.ForceFreshSession {
				t.Errorf("force_fresh_session = true, want false (provider_network resumes) for %s", util.UUIDToString(child.ID))
			}
			if !child.ChannelContextRevision.Valid || child.ChannelContextRevision.Int64 != 7 {
				t.Errorf("channel_context_revision = %+v, want 7", child.ChannelContextRevision)
			}
		})
	}
}

// TestRuntimeOfflineRetryWaitsForHealthyRuntime covers the complete recovery
// boundary: runtime_offline creates a deferred child, an offline runtime cannot
// promote or claim it even after fire_at is due, and a fresh online heartbeat
// releases that exact child without consuming another retry.
func TestRuntimeOfflineRetryWaitsForHealthyRuntime(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE agent_runtime SET status='online', last_seen_at=now() WHERE id=$1`, runtimeID)
	})
	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status='offline', last_seen_at=now() - interval '4 hours' WHERE id=$1`, runtimeID); err != nil {
		t.Fatalf("mark runtime offline: %v", err)
	}

	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, 1, 2, 'src-session', '/tmp/src-workdir')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	svc := NewTaskService(q, pool, nil, events.New())
	if _, err := svc.FailTask(ctx, parentID, "runtime went offline", "src-session", "/tmp/src-workdir", "", "runtime_offline", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var childID pgtype.UUID
	var childStatus string
	var fireAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `
		SELECT id, status, fire_at FROM agent_task_queue WHERE parent_task_id = $1
	`, parentID).Scan(&childID, &childStatus, &fireAt); err != nil {
		t.Fatalf("read retry child: %v", err)
	}
	if childStatus != "deferred" || !fireAt.Valid {
		t.Fatalf("runtime_offline child = status:%q fire_at_valid:%v, want deferred with fire_at", childStatus, fireAt.Valid)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET fire_at=now() - interval '1 second' WHERE id=$1`, childID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}

	claimed, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(runtimeID))
	if err != nil {
		t.Fatalf("claim while offline: %v", err)
	}
	if claimed != nil {
		t.Fatalf("offline runtime claimed retry %s", util.UUIDToString(claimed.ID))
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, childID).Scan(&childStatus); err != nil {
		t.Fatalf("read child after offline claim: %v", err)
	}
	if childStatus != "deferred" {
		t.Fatalf("offline claim changed child to %q, want deferred", childStatus)
	}

	if _, err := pool.Exec(ctx, `UPDATE agent_runtime SET status='online', last_seen_at=now() WHERE id=$1`, runtimeID); err != nil {
		t.Fatalf("restore runtime heartbeat: %v", err)
	}
	claimed, err = svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(runtimeID))
	if err != nil {
		t.Fatalf("claim after reconnect: %v", err)
	}
	if claimed == nil || claimed.ID != childID {
		got := "nil"
		if claimed != nil {
			got = util.UUIDToString(claimed.ID)
		}
		t.Fatalf("claimed task = %s, want retry child %s", got, util.UUIDToString(childID))
	}
}

func TestMaybeRetryFailedTaskCopiesChannelDelivery(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID, sessionID, installationID, bindingID, parentID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'channel retry') RETURNING id
	`, workspaceID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'slack', '{}'::jsonb, 'active', $3) RETURNING id
	`, workspaceID, agentID, userID).Scan(&installationID); err != nil {
		t.Fatalf("insert channel installation: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_chat_session_binding (
			chat_session_id, installation_id, channel_type, channel_chat_id, chat_type,
			last_message_id, last_thread_id, route_revision, config
		) VALUES ($1, $2, 'slack', 'D123', 'p2p', '171.001', '171.001', 4, '{"channel_id":"D123"}'::jsonb)
		RETURNING id
	`, sessionID, installationID).Scan(&bindingID); err != nil {
		t.Fatalf("insert channel binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id, status, priority, attempt,
			max_attempts, failure_reason, originator_user_id, accountable_user_id, originator_source
		) VALUES ($1, $2, $3, 'failed', 0, 1, 3, 'timeout', $4, $4, 'direct_human')
		RETURNING id
	`, agentID, runtimeID, sessionID, userID).Scan(&parentID); err != nil {
		t.Fatalf("insert failed channel task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_task_delivery (
			task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type,
			channel_message_id, channel_thread_id, route_revision, config
		) VALUES ($1, $2, $3, 'slack', 'D123', 'p2p', '171.001', '171.001', 4, '{"channel_id":"D123"}'::jsonb)
	`, parentID, bindingID, installationID); err != nil {
		t.Fatalf("insert parent delivery: %v", err)
	}

	parent, err := q.GetAgentTask(ctx, util.MustParseUUID(parentID))
	if err != nil {
		t.Fatalf("load parent: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	child, err := svc.MaybeRetryFailedTask(ctx, parent)
	if err != nil {
		t.Fatalf("MaybeRetryFailedTask: %v", err)
	}
	if child == nil {
		t.Fatal("expected retry child")
	}
	delivery, err := q.GetChannelTaskDelivery(ctx, child.ID)
	if err != nil {
		t.Fatalf("load retry delivery: %v", err)
	}
	if util.UUIDToString(delivery.BindingID) != bindingID ||
		util.UUIDToString(delivery.InstallationID) != installationID ||
		delivery.ChannelType != "slack" || delivery.ChannelChatID != "D123" ||
		delivery.RouteRevision != 4 || delivery.ChannelMessageID.String != "171.001" {
		t.Fatalf("retry delivery drifted from parent: %+v", delivery)
	}

	// Exercise the independent FailTask retry path as well. It creates the
	// child inside the same transaction that marks this attempt failed, and its
	// delivery copy must commit atomically with that child.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, child.ID); err != nil {
		t.Fatalf("mark retry running: %v", err)
	}
	if _, err := svc.FailTask(ctx, child.ID, "runtime timed out", "", "", "", "timeout", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	var grandchildID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue WHERE parent_task_id = $1 ORDER BY created_at DESC LIMIT 1
	`, child.ID).Scan(&grandchildID); err != nil {
		t.Fatalf("load FailTask retry child: %v", err)
	}
	grandchildDelivery, err := q.GetChannelTaskDelivery(ctx, grandchildID)
	if err != nil {
		t.Fatalf("load FailTask retry delivery: %v", err)
	}
	if util.UUIDToString(grandchildDelivery.BindingID) != bindingID || grandchildDelivery.RouteRevision != 4 {
		t.Fatalf("FailTask retry delivery drifted from parent: %+v", grandchildDelivery)
	}
}

// TestFailTaskProviderNetworkBudget is the end-to-end guard for Elon's must-fix
// (MUL-4910): FailTask must (1) grant provider_network its raised budget and
// persist a self-consistent child (attempt=3, max_attempts=3), and (2) still
// honour max_attempts=1 as "auto-retry disabled" — no child at all.
func TestFailTaskProviderNetworkBudget(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	cases := []struct {
		name         string
		attempt      int32
		maxAttempts  int32
		wantChild    bool
		wantAttempt  int32
		wantMax      int32
		wantDeferred bool
	}{
		// Default budget, failing on the 2nd attempt → deferred final tier that
		// records attempt=3 AND max_attempts=3 (no contradictory row).
		{"final tier persists raised budget", 2, 2, true, 3, 3, true},
		// Default budget, failing on the 1st attempt → immediate 2nd tier.
		{"first retry is immediate", 1, 2, true, 2, 3, false},
		// max_attempts=1 disables auto-retry — even provider_network gets none.
		{"disabled budget is never revived", 1, 1, false, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID pgtype.UUID
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, session_id, work_dir)
				VALUES ($1, $2, $3, 'running', 0, $4, $5, 'src-session', '/tmp/src-workdir')
				RETURNING id
			`, agentID, runtimeID, issueID, tc.attempt, tc.maxAttempts).Scan(&parentID); err != nil {
				t.Fatalf("insert parent task: %v", err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
			})

			if _, err := svc.FailTask(ctx, parentID, "API Error: Connection closed mid-response.", "src-session", "/tmp/src-workdir", "", "agent_error.provider_network", false, "", ""); err != nil {
				t.Fatalf("FailTask: %v", err)
			}

			var (
				childAttempt, childMax int32
				childStatus            string
				n                      int
			)
			row := pool.QueryRow(ctx, `SELECT count(*), coalesce(max(attempt),0), coalesce(max(max_attempts),0), coalesce(max(status),'') FROM agent_task_queue WHERE parent_task_id = $1`, parentID)
			if err := row.Scan(&n, &childAttempt, &childMax, &childStatus); err != nil {
				t.Fatalf("read child: %v", err)
			}
			if !tc.wantChild {
				if n != 0 {
					t.Fatalf("expected no retry child, got %d", n)
				}
				return
			}
			if n != 1 {
				t.Fatalf("expected exactly one retry child, got %d", n)
			}
			if childAttempt != tc.wantAttempt {
				t.Errorf("child attempt = %d, want %d", childAttempt, tc.wantAttempt)
			}
			if childMax != tc.wantMax {
				t.Errorf("child max_attempts = %d, want %d (self-consistent budget)", childMax, tc.wantMax)
			}
			gotDeferred := childStatus == "deferred"
			if gotDeferred != tc.wantDeferred {
				t.Errorf("child status = %q, want deferred=%v", childStatus, tc.wantDeferred)
			}
		})
	}
}
