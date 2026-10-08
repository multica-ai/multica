package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRerunIssueBlockedBeforeMutationWhenInvokeDenied is the security acceptance
// test for MUL-4525 §5: a rerun whose operator cannot invoke the resolved target
// agent must be refused with ErrRerunInvokeNotAllowed, and it must fail BEFORE
// any mutation — the prior task is not cancelled and no new task is created.
func TestRerunIssueBlockedBeforeMutationWhenInvokeDenied(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)

	issueStruct := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(creatorID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	orig, err := svc.EnqueueTaskForIssue(ctx, issueStruct)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue (original): %v", err)
	}

	countTasks := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}
	origStatus := func() string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, orig.ID).Scan(&s); err != nil {
			t.Fatalf("read orig status: %v", err)
		}
		return s
	}
	beforeCount := countTasks()
	beforeStatus := origStatus()

	// The gate is invoked with the RESOLVED target agent and denies it.
	gateSawAgent := false
	deny := func(a db.Agent) bool {
		if util.UUIDToString(a.ID) == agentID {
			gateSawAgent = true
		}
		return false
	}

	_, err = svc.RerunIssue(ctx, util.MustParseUUID(issueID), orig.ID, pgtype.UUID{}, util.MustParseUUID(creatorID), deny)
	if !errors.Is(err, ErrRerunInvokeNotAllowed) {
		t.Fatalf("RerunIssue with denying gate: err = %v, want ErrRerunInvokeNotAllowed", err)
	}
	if !gateSawAgent {
		t.Errorf("gate was not evaluated against the resolved target agent %s", agentID)
	}
	// Fail-before-mutation: no new task, original untouched.
	if got := countTasks(); got != beforeCount {
		t.Errorf("task count changed after blocked rerun: got %d, want %d", got, beforeCount)
	}
	if got := origStatus(); got != beforeStatus {
		t.Errorf("original task status changed after blocked rerun: got %q, want %q", got, beforeStatus)
	}

	// A permitting gate reruns normally (cancels the original, enqueues fresh).
	allow := func(db.Agent) bool { return true }
	rerun, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), orig.ID, pgtype.UUID{}, util.MustParseUUID(creatorID), allow)
	if err != nil {
		t.Fatalf("RerunIssue with permitting gate: %v", err)
	}
	if util.UUIDToString(rerun.ID) == util.UUIDToString(orig.ID) {
		t.Errorf("expected a new task id, got the original %s", util.UUIDToString(orig.ID))
	}
}

// TestAutopilotDispatchAdmitsClickerNotCreator is the acceptance test for
// MUL-4525 §3: a MANUAL "run now" admits on the invoke permission of the human
// who ordered it (not the autopilot creator's), while a dispatch that resolves
// no principal at all is refused. The two must not fork.
func TestAutopilotDispatchAdmitsClickerNotCreator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	// Fixture agent is private and owned by ownerID.
	workspaceID, ownerID, agentID, _ := seedAttributionFixture(t, pool)

	// A different member owns the autopilot; they neither own the private agent
	// nor sit on its allow-list, so the creator gate denies them.
	var apCreatorID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('AP Creator', $1) RETURNING id`,
		fmt.Sprintf("apc-%d@multica.test", time.Now().UnixNano())).Scan(&apCreatorID); err != nil {
		t.Fatalf("seed ap creator: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, apCreatorID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		workspaceID, apCreatorID); err != nil {
		t.Fatalf("seed ap creator member: %v", err)
	}

	ap := db.Autopilot{
		WorkspaceID:   util.MustParseUUID(workspaceID),
		AssigneeID:    util.MustParseUUID(agentID),
		AssigneeType:  "agent",
		ExecutionMode: "run_only",
		Status:        "active",
		CreatedByType: "member",
		CreatedByID:   util.MustParseUUID(apCreatorID),
	}
	svc := &AutopilotService{Queries: q}

	// Manual dispatch by the agent owner is admitted. actorUserID is the human who
	// ORDERED the run — the clicking member, or the originator an agent acts for;
	// the handler resolves and authorizes it (requireAutopilotTriggerInvoker) before
	// this gate ever sees it.
	if reason, _, skip := svc.shouldSkipDispatch(ctx, ap, util.MustParseUUID(ownerID), pgtype.UUID{}); skip {
		t.Fatalf("manual dispatch by the agent owner should be admitted, got skip: %q", reason)
	}

	// Neither an actor nor a trigger: no principal exists to judge, so the gate
	// denies. This argument shape reaches the service only from an automation path
	// that lost its trigger — the manual entry point is refused upstream (#8078).
	//
	// The reason must NOT blame a trigger owner here. Until #8078 it did, and
	// because a manual "run now" passes exactly this pair (no member actor, no
	// trigger row), every agent-initiated trigger reported a broken trigger owner
	// for a trigger that never existed — which is what made the regression cost a
	// day to diagnose. Assert the phrasing, not just the skip.
	reason, code, skip := svc.shouldSkipDispatch(ctx, ap, pgtype.UUID{}, pgtype.UUID{})
	if !skip {
		t.Fatalf("dispatch with no resolvable principal should be blocked")
	}
	if !strings.Contains(strings.ToLower(reason), "no authorizing human") {
		t.Errorf("no-principal skip reason = %q, want the no-authorizing-human phrasing", reason)
	}
	if strings.Contains(strings.ToLower(reason), "trigger's owner") {
		t.Errorf("no-principal skip reason = %q, must not blame a trigger row that does not exist", reason)
	}
	if code != dispatch.ReasonInvocationNotAllowed {
		t.Errorf("skip reason_code = %q, want invocation_not_allowed", code)
	}
}
