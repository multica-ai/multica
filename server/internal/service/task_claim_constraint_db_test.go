package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-backed coverage for the claim constraint policy SPI. These tests follow
// the package's sharedTestPool convention (same as task_claim_decision_db_test.go):
// without a reachable Postgres they skip, so `go test ./...` stays usable
// without a database. Every case builds its own fixture; testutil.Insert
// registers reverse-order cleanup so cases never share rows.

// claimConstraintFixture is one workspace / runtime / agent / issue plus a
// TaskService wired to real metrics and a claim constraint policy, so each
// case can assert both that dispatch is unchanged (shadow-only) and exactly
// which (claim_path, decision, reason) series the evaluation produced.
type claimConstraintFixture struct {
	svc       *TaskService
	metrics   *obsmetrics.BusinessMetrics
	stub      *stubClaimConstraintPolicy
	fx        *testutil.Fixture
	pool      *pgxpool.Pool
	agentID   pgtype.UUID
	runtimeID pgtype.UUID
	issueID   string
}

// newClaimConstraintFixture wires the given policy into the service. Passing
// nil keeps ClaimConstraints nil, which is the default-deployment regression
// case.
func newClaimConstraintFixture(t *testing.T, policy ClaimConstraintPolicy) *claimConstraintFixture {
	t.Helper()

	pool := sharedTestPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	ownerID := bootstrap.User(t,
		fmt.Sprintf("claim-constraint-owner-%d", suffix),
		fmt.Sprintf("claim-constraint-owner-%d@example.com", suffix),
	)
	workspaceID := bootstrap.Workspace(t,
		fmt.Sprintf("claim-constraint-%d", suffix),
		fmt.Sprintf("claim-constraint-%d", suffix),
	)
	fx := testutil.New(pool, workspaceID, ownerID)
	fx.Member(t, workspaceID, ownerID, "owner")
	runtimeID := fx.Runtime(t, "claim-constraint-runtime", testutil.Cols{
		"visibility": "public",
		"owner_id":   ownerID,
	})
	agentID := fx.Agent(t, "claim-constraint-agent", runtimeID, testutil.Cols{
		"owner_id":             ownerID,
		"max_concurrent_tasks": 5,
	})
	issueID := fx.Issue(t, "claim constraint issue")

	m := obsmetrics.NewBusinessMetrics()
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.Metrics = m
	svc.ClaimConstraints = policy

	var stub *stubClaimConstraintPolicy
	if s, ok := policy.(*stubClaimConstraintPolicy); ok {
		stub = s
	}
	return &claimConstraintFixture{
		svc:       svc,
		metrics:   m,
		stub:      stub,
		fx:        fx,
		pool:      pool,
		agentID:   util.MustParseUUID(agentID),
		runtimeID: util.MustParseUUID(runtimeID),
		issueID:   issueID,
	}
}

// insertTask queues a task for the fixture agent on the fixture runtime.
func (f *claimConstraintFixture) insertTask(t *testing.T) string {
	t.Helper()

	return f.fx.Task(t, util.UUIDToString(f.agentID), testutil.Cols{
		"runtime_id": util.UUIDToString(f.runtimeID),
		"issue_id":   f.issueID,
		"status":     "queued",
	})
}

// TestClaimConstraintDBShadowDoesNotBlockClaim is the core assertion of the
// SPI: a policy that denies everything must not change dispatch. The claim
// still succeeds, the policy ran exactly once on the head candidate, and the
// decision is recorded — with the stub's unknown reason normalized to "other".
func TestClaimConstraintDBShadowDoesNotBlockClaim(t *testing.T) {
	stub := &stubClaimConstraintPolicy{resp: ClaimConstraintDecision{Action: claimConstraintDeny, Reason: "x"}}
	f := newClaimConstraintFixture(t, stub)
	taskID := f.insertTask(t)

	claimed, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimTaskForRuntime returned nil, want the queued task despite a denying policy")
	}
	if stub.calls != 1 {
		t.Fatalf("policy evaluated %d times, want 1", stub.calls)
	}
	if got := util.UUIDToString(stub.last.TaskID); got != taskID {
		t.Fatalf("policy saw task_id %s, want the claimed task %s", got, taskID)
	}
	if stub.last.AgentID != f.agentID || stub.last.RuntimeID != f.runtimeID {
		t.Fatalf("policy saw agent/runtime %+v/%+v, want the fixture pair", stub.last.AgentID, stub.last.RuntimeID)
	}
	if stub.last.ClaimPath != claimPathRuntime || stub.last.Candidates != 1 {
		t.Fatalf("policy saw claim_path=%q candidates=%d, want runtime/1", stub.last.ClaimPath, stub.last.Candidates)
	}
	if got := claimConstraintCount(t, f.metrics, claimPathRuntime, claimConstraintDeny, claimConstraintReasonOther); got != 1 {
		t.Fatalf("claim_constraint_decision_total{runtime,deny,other} = %v, want 1", got)
	}
}

// TestClaimConstraintDBCanaryAllow wires the built-in canary policy and covers
// the claimed happy path: the task is claimed and the decision lands on
// (runtime, allow, canary).
func TestClaimConstraintDBCanaryAllow(t *testing.T) {
	f := newClaimConstraintFixture(t, ClaimConstraintCanaryPolicy{})
	f.insertTask(t)

	claimed, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimTaskForRuntime returned nil, want the queued task")
	}
	if got := claimConstraintCount(t, f.metrics, claimPathRuntime, claimConstraintAllow, claimConstraintReasonCanary); got != 1 {
		t.Fatalf("claim_constraint_decision_total{runtime,allow,canary} = %v, want 1", got)
	}
}

// TestClaimConstraintDBBatchWait covers the batch poll: a waiting policy
// neither blocks the batch claim nor leaks into the runtime path — the task is
// still claimed and only (batch, wait, other) increments (unknown reason
// folds to other).
func TestClaimConstraintDBBatchWait(t *testing.T) {
	stub := &stubClaimConstraintPolicy{resp: ClaimConstraintDecision{Action: claimConstraintWait, Reason: "later"}}
	f := newClaimConstraintFixture(t, stub)
	f.insertTask(t)

	claimed, err := f.svc.ClaimTasksForRuntimes(context.Background(), []pgtype.UUID{f.runtimeID}, 8)
	if err != nil {
		t.Fatalf("ClaimTasksForRuntimes: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimTasksForRuntimes claimed %d tasks, want 1 despite the waiting policy", len(claimed))
	}
	if stub.calls != 1 {
		t.Fatalf("policy evaluated %d times, want 1", stub.calls)
	}
	if stub.last.ClaimPath != claimPathBatch || stub.last.Candidates != 1 {
		t.Fatalf("policy saw claim_path=%q candidates=%d, want batch/1", stub.last.ClaimPath, stub.last.Candidates)
	}
	if got := claimConstraintCount(t, f.metrics, claimPathBatch, claimConstraintWait, claimConstraintReasonOther); got != 1 {
		t.Fatalf("claim_constraint_decision_total{batch,wait,other} = %v, want 1", got)
	}
	if got := claimConstraintCount(t, f.metrics, claimPathRuntime, claimConstraintWait, claimConstraintReasonOther); got != 0 {
		t.Fatalf("claim_constraint_decision_total{runtime,wait,other} = %v, want 0", got)
	}
}

// TestClaimConstraintDBIdlePollRecordsNothing pins the funnel boundary: the
// policy is only consulted at the candidate stage, so an idle poll (no
// candidates) never evaluates anything — the constraint family stays at zero
// while the sibling claim decision funnel still records its own empty_db
// outcome for the same poll.
func TestClaimConstraintDBIdlePollRecordsNothing(t *testing.T) {
	f := newClaimConstraintFixture(t, ClaimConstraintCanaryPolicy{})

	claimed, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed != nil {
		t.Fatal("ClaimTaskForRuntime claimed a task, want an idle poll")
	}
	for _, s := range claimConstraintZeroSeries(t, f.metrics) {
		if s.value != 0 {
			t.Fatalf("claim_constraint series {%s,%s,%s} = %v after an idle poll, want 0", s.claimPath, s.decision, s.reason, s.value)
		}
	}
	if got := claimDecisionCount(t, f.metrics, claimPathRuntime, claimOutcomeEmptyDB, claimReasonNoVisibleCandidates); got != 1 {
		t.Fatalf("claim_decision_total{runtime,empty_db,no_visible_candidates} = %v, want 1", got)
	}
}

// TestClaimConstraintDBNilPolicyIsTransparent is the zero-behavior-change
// regression: with ClaimConstraints nil (the default), the claim still works
// and no claim_constraint series moves at all.
func TestClaimConstraintDBNilPolicyIsTransparent(t *testing.T) {
	f := newClaimConstraintFixture(t, nil)
	f.insertTask(t)

	claimed, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimTaskForRuntime returned nil, want the queued task")
	}
	for _, s := range claimConstraintZeroSeries(t, f.metrics) {
		if s.value != 0 {
			t.Fatalf("claim_constraint series {%s,%s,%s} = %v with nil policy, want 0", s.claimPath, s.decision, s.reason, s.value)
		}
	}
}
