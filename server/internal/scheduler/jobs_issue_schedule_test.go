package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubIssueScheduleDispatcher satisfies IssueScheduleDispatcher without
// touching the DB — used only to build a JobSpec for shape assertions, never
// invoked (the handler/plan tests below call the unexported hooks directly).
type stubIssueScheduleDispatcher struct{}

func (stubIssueScheduleDispatcher) DispatchIssueSchedule(context.Context, db.IssueScheduledTrigger) error {
	return nil
}

// The job spec must satisfy the scheduler's own invariants (RunTimeout <
// StaleTimeout, HeartbeatInterval < StaleTimeout, MaxAttempts >= 1, etc.) —
// see JobSpec.validate in spec.go. A future edit to the tuning constants in
// IssueScheduleDispatchJob that breaks one of these would otherwise only be
// caught by a Register() call at process boot.
func TestIssueScheduleDispatchJobSpecIsValid(t *testing.T) {
	job := IssueScheduleDispatchJob(nil, nil, stubIssueScheduleDispatcher{})
	if err := job.validate(); err != nil {
		t.Fatalf("IssueScheduleDispatchJob produced an invalid JobSpec: %v", err)
	}
	if job.Name != JobNameIssueScheduleDispatch {
		t.Fatalf("Name = %q, want %q", job.Name, JobNameIssueScheduleDispatch)
	}
	// MaxAttempts > 1 exists only for this process's own crash recovery
	// (see the doc comment on IssueScheduleDispatchJob) — not to retry a
	// missed fire, which DispatchIssueSchedule resolves on the first
	// attempt. A regression to MaxAttempts=1 would silently drop that
	// crash-recovery guarantee.
	if job.MaxAttempts < 2 {
		t.Fatalf("MaxAttempts = %d, want >= 2 (crash-recovery retry)", job.MaxAttempts)
	}
}

func TestIssueSchedulePlansForScopeReturnsCachedRunAt(t *testing.T) {
	cache := newIssueScheduleCache()
	runAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	triggerID := "11111111-1111-1111-1111-111111111111"
	cache.replace(map[string]db.IssueScheduledTrigger{
		triggerID: {
			Status: "pending",
			RunAt:  pgtype.Timestamptz{Time: runAt, Valid: true},
		},
	})

	plans, err := issueSchedulePlansForScope(cache)(
		context.Background(),
		Scope{Kind: ScopeKindIssueScheduledTrigger, ID: triggerID},
		runAt,
		LatestPlanInfo{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 1 || !plans[0].Equal(runAt) {
		t.Fatalf("plans = %v, want [%v]", plans, runAt)
	}
}

// A scope that vanished from the cache between Scopes and PlansForScope
// (fired, cancelled, or deleted by a concurrent request) must plan nothing —
// not error, not fall back to some other time.
func TestIssueSchedulePlansForScopeMissingScopeReturnsNil(t *testing.T) {
	cache := newIssueScheduleCache()

	plans, err := issueSchedulePlansForScope(cache)(
		context.Background(),
		Scope{Kind: ScopeKindIssueScheduledTrigger, ID: "does-not-exist"},
		time.Now(),
		LatestPlanInfo{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plans != nil {
		t.Fatalf("plans = %v, want nil", plans)
	}
}

func TestIssueScheduleCacheGetAfterReplace(t *testing.T) {
	cache := newIssueScheduleCache()
	if _, ok := cache.get("missing"); ok {
		t.Fatal("get on empty cache should report ok=false")
	}

	trigger := db.IssueScheduledTrigger{Status: "pending"}
	cache.replace(map[string]db.IssueScheduledTrigger{"a": trigger})
	if _, ok := cache.get("a"); !ok {
		t.Fatal("get should find the id just replaced in")
	}

	// A second replace fully swaps the set — a stale id from the previous
	// tick must not linger.
	cache.replace(map[string]db.IssueScheduledTrigger{"b": trigger})
	if _, ok := cache.get("a"); ok {
		t.Fatal("get should not find an id from a previous replace()")
	}
	if _, ok := cache.get("b"); !ok {
		t.Fatal("get should find the id from the latest replace()")
	}
}
