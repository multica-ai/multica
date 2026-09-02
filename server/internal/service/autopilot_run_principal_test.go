package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MUL-6951 (Elon review). One automatic dispatch must resolve exactly ONE human
// and use it for everything: admitting the first agent, stamping the task, and
// every run delegated from it.
//
// The bug these tests exist for: admission used to ask "may the AUTOPILOT CREATOR
// invoke this agent?" while the task took its identity from the trigger. With A
// owning both the autopilot and a private agent, and B owning the trigger, the
// dispatch was admitted as A and then executed as B — a combination neither of
// them can produce by hand. The previous suite missed it because it called
// dispatchRunOnly directly, which skips admission entirely; these drive
// DispatchAutopilot so the gate actually runs.

// seedPrivateAgentOwnedBy creates a PRIVATE agent, so only its owner may invoke
// it and the gate's verdict names exactly which human was consulted.
func seedPrivateAgentOwnedBy(t *testing.T, pool *pgxpool.Pool, workspaceID, ownerID, label string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID, agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, $2, 'cloud', 'codex', 'online', '', '{}'::jsonb, $3) RETURNING id`,
		workspaceID, "rt-"+label, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args, permission_mode)
		VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, 'private')
		RETURNING id`, workspaceID, "agent-"+label, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("seed private agent: %v", err)
	}
	return agentID
}

// seedAutopilotWithTrigger wires a run_only autopilot created by apCreatorID over
// agentID, with one schedule trigger created by trigCreatorID. The two creators
// are independent — that separation is where the fork lived.
func seedAutopilotWithTrigger(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, apCreatorID, trigCreatorID string) (string, string) {
	t.Helper()
	ctx := context.Background()
	var autopilotID, triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
		VALUES ($1, $2, 'agent', $3, 'active', 'run_only', 'member', $4) RETURNING id`,
		workspaceID, "principal-ap-"+t.Name(), agentID, apCreatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression,
			published_by_type, published_by_id, created_by_type, created_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2, 'member', $2) RETURNING id`,
		autopilotID, trigCreatorID).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	return autopilotID, triggerID
}

func newPrincipalService(pool *pgxpool.Pool, q *db.Queries) *AutopilotService {
	return &AutopilotService{
		Queries: q, TxStarter: pool, Bus: events.New(),
		TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()},
	}
}

func TestAutopilotDispatch_AdmitsAsTheTriggerCreatorNotTheAutopilotCreator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, _, _ := seedAttributionFixture(t, pool)
	triggerOwnerB := seedExtraMember(t, pool, workspaceID, "principal-b")

	// THE FORK: the private agent and the autopilot both belong to A; the trigger
	// belongs to B. Admission used to consult A and pass, while the run took B's
	// identity. It must now consult B — who cannot invoke A's private agent.
	agentID := seedPrivateAgentOwnedBy(t, pool, workspaceID, creatorA, "fork-a")
	autopilotID, triggerID := seedAutopilotWithTrigger(t, pool, workspaceID, agentID, creatorA, triggerOwnerB)

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	run, err := newPrincipalService(pool, q).DispatchAutopilot(ctx, ap, util.MustParseUUID(triggerID), "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || run.Status != "skipped" {
		t.Fatalf("dispatch = %+v, want skipped: the trigger's creator cannot invoke the autopilot creator's private agent", run)
	}
	if !run.FailureReason.Valid {
		t.Error("a refused dispatch must record why")
	}
	var tasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 0 {
		t.Fatalf("refused dispatch still enqueued %d tasks", tasks)
	}
}

func TestAutopilotDispatch_TriggerCreatorOwningTheAgentIsAdmittedAndStamped(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, _, _ := seedAttributionFixture(t, pool)
	triggerOwnerB := seedExtraMember(t, pool, workspaceID, "principal-owner-b")

	// The mirror image: B owns both the trigger and the private agent, while the
	// autopilot belongs to A. Admission must consult B and pass, and the task must
	// be stamped with B — proving both halves read the same human.
	agentID := seedPrivateAgentOwnedBy(t, pool, workspaceID, triggerOwnerB, "mirror-b")
	autopilotID, triggerID := seedAutopilotWithTrigger(t, pool, workspaceID, agentID, creatorA, triggerOwnerB)

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	run, err := newPrincipalService(pool, q).DispatchAutopilot(ctx, ap, util.MustParseUUID(triggerID), "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || run.Status == "skipped" {
		reason := ""
		if run != nil {
			reason = run.FailureReason.String
		}
		t.Fatalf("dispatch was refused (%q), want admitted: the trigger creator owns the agent", reason)
	}

	var originator, accountable pgtype.UUID
	var source pgtype.Text
	if err := pool.QueryRow(ctx, `
		SELECT originator_user_id, accountable_user_id, originator_source
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&originator, &accountable, &source); err != nil {
		t.Fatalf("read dispatched task: %v", err)
	}
	if source.String != "trigger_owner" {
		t.Errorf("originator_source = %q, want trigger_owner", source.String)
	}
	// The whole point: the human admission consulted is the human the task carries.
	if util.UUIDToString(originator) != triggerOwnerB {
		t.Errorf("originator = %q, want the admitted trigger creator %q", util.UUIDToString(originator), triggerOwnerB)
	}
	if accountable != originator {
		t.Errorf("accountable %q must equal originator %q", util.UUIDToString(accountable), util.UUIDToString(originator))
	}
}

func TestResolveAutopilotTriggerPrincipal_FailsClosed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, _ := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, creatorID)

	seq := 0
	seedTrigger := func(createdByType, createdByID any) pgtype.UUID {
		seq++
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression,
				published_by_type, published_by_id, created_by_type, created_by_id)
			VALUES ($1, 'schedule', true, $2, 'member', $3, $4, $5) RETURNING id`,
			autopilotID, fmt.Sprintf("%d * * * *", seq), creatorID, createdByType, createdByID).Scan(&id); err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		return util.MustParseUUID(id)
	}

	wsUUID := util.MustParseUUID(workspaceID)
	apUUID := util.MustParseUUID(autopilotID)

	t.Run("resolves the trigger creator", func(t *testing.T) {
		got := ResolveAutopilotTriggerPrincipal(ctx, q, seedTrigger("member", creatorID), apUUID, wsUUID)
		if util.UUIDToString(got) != creatorID {
			t.Fatalf("principal = %q, want %q", util.UUIDToString(got), creatorID)
		}
	})

	t.Run("legacy trigger with no creator resolves nobody", func(t *testing.T) {
		// published_by IS set here, so the pre-MUL-6951 resolver would have returned
		// a human. It must not be promoted to an authorization principal — that is
		// the rule_owner guess, and guessing is what fails closed now.
		if got := ResolveAutopilotTriggerPrincipal(ctx, q, seedTrigger(nil, nil), apUUID, wsUUID); got.Valid {
			t.Fatalf("legacy trigger resolved %q; must fail closed", util.UUIDToString(got))
		}
	})

	t.Run("trigger belonging to another autopilot resolves nobody", func(t *testing.T) {
		otherAutopilotID, _ := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, creatorID)
		got := ResolveAutopilotTriggerPrincipal(ctx, q, seedTrigger("member", creatorID), util.MustParseUUID(otherAutopilotID), wsUUID)
		if got.Valid {
			t.Fatalf("cross-autopilot trigger resolved %q; the binding check did not run", util.UUIDToString(got))
		}
	})

	t.Run("creator removed from the workspace resolves nobody", func(t *testing.T) {
		removed := seedExtraMember(t, pool, workspaceID, "principal-removed")
		triggerID := seedTrigger("member", removed)
		if got := ResolveAutopilotTriggerPrincipal(ctx, q, triggerID, apUUID, wsUUID); !got.Valid {
			t.Fatal("precondition: an in-workspace creator must resolve")
		}
		if _, err := pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, removed); err != nil {
			t.Fatalf("remove member: %v", err)
		}
		if got := ResolveAutopilotTriggerPrincipal(ctx, q, triggerID, apUUID, wsUUID); got.Valid {
			t.Fatalf("removed member still resolved %q; membership must be re-checked per dispatch", util.UUIDToString(got))
		}
	})
}
