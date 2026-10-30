package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
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

// principalSeq keeps generated names and emails unique across parallel subtests.
var principalSeq atomic.Int64

// principalFixture is the service-package equivalent of the handler suite's dbfx:
// every row these tests create goes through testutil.Fixture, which registers its
// own cleanup, so this file open-codes no INSERT / DELETE pairs.
//
// It deliberately does NOT reuse this package's seedAttributionFixture: that
// helper raw-inserts its user / workspace / member / runtime / agent with matching
// hand-written cleanups, which is the pattern CLAUDE.md prohibits for new tests,
// and it builds an agent and issue these tests do not use.
type principalFixture struct {
	*testutil.Fixture
	svc *AutopilotService
	q   *db.Queries
}

func newPrincipalFixture(t *testing.T) (principalFixture, string) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	q := db.New(pool)

	// Bootstrap: Fixture.User and Fixture.Workspace do not read the fixture's own
	// WorkspaceID / UserID, so an unbound fixture can create the base identity and
	// then adopt it. Cleanup runs in reverse creation order, so the member row goes
	// before the workspace and user it references.
	n := principalSeq.Add(1)
	fx := testutil.New(pool, "", "")
	ownerUserID := fx.User(t, "principal owner", fmt.Sprintf("principal-owner-%d@multica.test", n))
	workspaceID := fx.Workspace(t, "principal ws", fmt.Sprintf("principal-ws-%d", n))
	fx.Member(t, workspaceID, ownerUserID, "owner")
	fx.WorkspaceID = workspaceID
	fx.UserID = ownerUserID

	return principalFixture{
		Fixture: fx,
		q:       q,
		svc: &AutopilotService{
			Queries: q, TxStarter: pool, Bus: events.New(),
			TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()},
		},
	}, ownerUserID
}

// member adds a fresh workspace member and returns their user id.
func (f principalFixture) member(t *testing.T, label string) string {
	t.Helper()
	n := principalSeq.Add(1)
	userID := f.User(t, label, fmt.Sprintf("%s-%d@multica.test", label, n))
	f.Member(t, f.WorkspaceID, userID, "member")
	return userID
}

// privateAgentOwnedBy builds a PRIVATE agent, so only its owner may invoke it and
// the gate's verdict names exactly which human was consulted. Fixture.Agent
// already defaults permission_mode/visibility to private; only the owner moves.
func (f principalFixture) privateAgentOwnedBy(t *testing.T, ownerID, label string) string {
	t.Helper()
	n := principalSeq.Add(1)
	runtimeID := f.Runtime(t, fmt.Sprintf("rt-%s-%d", label, n), testutil.Cols{"owner_id": ownerID})
	return f.Agent(t, fmt.Sprintf("agent-%s-%d", label, n), runtimeID, testutil.Cols{"owner_id": ownerID})
}

// autopilotWithTrigger wires a run_only autopilot created by apCreatorID over
// agentID, with one schedule trigger created by trigCreatorID. The two creators
// are independent — that separation is where the fork lived.
func (f principalFixture) autopilotWithTrigger(t *testing.T, agentID, apCreatorID, trigCreatorID string) (string, string) {
	t.Helper()
	n := principalSeq.Add(1)
	autopilotID := f.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    f.WorkspaceID,
		"title":           fmt.Sprintf("principal-ap-%d", n),
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   apCreatorID,
	})
	triggerID := f.trigger(t, autopilotID, "member", trigCreatorID)
	return autopilotID, triggerID
}

// trigger inserts a schedule trigger. createdByType / createdByID are passed
// through untouched so a test can build the legacy (NULL) shape.
func (f principalFixture) trigger(t *testing.T, autopilotID string, createdByType, createdByID any) string {
	t.Helper()
	n := principalSeq.Add(1)
	return f.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":      autopilotID,
		"kind":              "schedule",
		"enabled":           true,
		"cron_expression":   fmt.Sprintf("%d * * * *", n%60),
		"published_by_type": "member",
		"published_by_id":   f.UserID,
		"created_by_type":   createdByType,
		"created_by_id":     createdByID,
	})
}

func (f principalFixture) dispatch(t *testing.T, autopilotID, triggerID string) *db.AutopilotRun {
	t.Helper()
	ap, err := f.q.GetAutopilot(context.Background(), util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	run, err := f.svc.DispatchAutopilot(context.Background(), ap, util.MustParseUUID(triggerID), "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("dispatch returned no run")
	}
	return run
}

func TestAutopilotDispatch_AdmitsAsTheTriggerCreatorNotTheAutopilotCreator(t *testing.T) {
	fx, creatorA := newPrincipalFixture(t)
	triggerOwnerB := fx.member(t, "principal-b")

	// THE FORK: the private agent and the autopilot both belong to A; the trigger
	// belongs to B. Admission used to consult A and pass, while the run took B's
	// identity. It must now consult B — who cannot invoke A's private agent.
	agentID := fx.privateAgentOwnedBy(t, creatorA, "fork-a")
	autopilotID, triggerID := fx.autopilotWithTrigger(t, agentID, creatorA, triggerOwnerB)

	run := fx.dispatch(t, autopilotID, triggerID)
	if run.Status != "skipped" {
		t.Fatalf("dispatch status = %q, want skipped: the trigger's creator cannot invoke the autopilot creator's private agent", run.Status)
	}
	if !run.FailureReason.Valid {
		t.Error("a refused dispatch must record why")
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID); n != 0 {
		t.Fatalf("refused dispatch still enqueued %d tasks", n)
	}
}

func TestAutopilotDispatch_TriggerCreatorOwningTheAgentIsAdmittedAndStamped(t *testing.T) {
	fx, creatorA := newPrincipalFixture(t)
	triggerOwnerB := fx.member(t, "principal-owner-b")

	// The mirror image: B owns both the trigger and the private agent, while the
	// autopilot belongs to A. Admission must consult B and pass, and the task must
	// be stamped with B — proving both halves read the same human.
	agentID := fx.privateAgentOwnedBy(t, triggerOwnerB, "mirror-b")
	autopilotID, triggerID := fx.autopilotWithTrigger(t, agentID, creatorA, triggerOwnerB)

	run := fx.dispatch(t, autopilotID, triggerID)
	if run.Status == "skipped" {
		t.Fatalf("dispatch was refused (%q), want admitted: the trigger creator owns the agent", run.FailureReason.String)
	}

	var originator, accountable pgtype.UUID
	var source pgtype.Text
	fx.QueryRow(t, `
		SELECT originator_user_id, accountable_user_id, originator_source
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&originator, &accountable, &source)

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
	fx, creatorID := newPrincipalFixture(t)
	ctx := context.Background()

	agentID := fx.privateAgentOwnedBy(t, creatorID, "resolve")
	autopilotID, _ := fx.autopilotWithTrigger(t, agentID, creatorID, creatorID)
	apUUID := util.MustParseUUID(autopilotID)
	wsUUID := util.MustParseUUID(fx.WorkspaceID)

	t.Run("resolves the trigger creator", func(t *testing.T) {
		triggerID := util.MustParseUUID(fx.trigger(t, autopilotID, "member", creatorID))
		if got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, wsUUID); util.UUIDToString(got) != creatorID {
			t.Fatalf("principal = %q, want %q", util.UUIDToString(got), creatorID)
		}
	})

	t.Run("legacy trigger with no creator resolves nobody", func(t *testing.T) {
		// published_by IS set here, so the pre-MUL-6951 resolver would have returned
		// a human. It must not be promoted to an authorization principal — that is
		// the rule_owner guess, and guessing is what fails closed now.
		triggerID := util.MustParseUUID(fx.trigger(t, autopilotID, nil, nil))
		if got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, wsUUID); got.Valid {
			t.Fatalf("legacy trigger resolved %q; must fail closed", util.UUIDToString(got))
		}
	})

	t.Run("trigger belonging to another autopilot resolves nobody", func(t *testing.T) {
		otherAutopilotID, _ := fx.autopilotWithTrigger(t, agentID, creatorID, creatorID)
		triggerID := util.MustParseUUID(fx.trigger(t, autopilotID, "member", creatorID))
		got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, util.MustParseUUID(otherAutopilotID), wsUUID)
		if got.Valid {
			t.Fatalf("cross-autopilot trigger resolved %q; the binding check did not run", util.UUIDToString(got))
		}
	})

	t.Run("autopilot in another workspace resolves nobody", func(t *testing.T) {
		// The membership check alone cannot catch this: it only proves the resolved
		// human belongs to the workspace passed in. The SAME human is a member of
		// BOTH workspaces here, so membership passes on either side and only the
		// autopilot-to-workspace binding in the lookup can reject (Elon review).
		n := principalSeq.Add(1)
		otherWorkspaceID := fx.Workspace(t, "other ws", fmt.Sprintf("other-ws-%d", n))
		fx.Member(t, otherWorkspaceID, creatorID, "owner")

		triggerID := util.MustParseUUID(fx.trigger(t, autopilotID, "member", creatorID))
		if got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, wsUUID); !got.Valid {
			t.Fatal("precondition: the trigger must resolve in its own workspace")
		}
		got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, util.MustParseUUID(otherWorkspaceID))
		if got.Valid {
			t.Fatalf("a foreign workspace resolved %q; the autopilot is not bound to it", util.UUIDToString(got))
		}
	})

	t.Run("creator removed from the workspace resolves nobody", func(t *testing.T) {
		removed := fx.member(t, "principal-removed")
		triggerID := util.MustParseUUID(fx.trigger(t, autopilotID, "member", removed))
		if got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, wsUUID); !got.Valid {
			t.Fatal("precondition: an in-workspace creator must resolve")
		}
		fx.Exec(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fx.WorkspaceID, removed)
		if got := ResolveAutopilotTriggerPrincipal(ctx, fx.q, triggerID, apUUID, wsUUID); got.Valid {
			t.Fatalf("removed member still resolved %q; membership must be re-checked per dispatch", util.UUIDToString(got))
		}
	})
}
