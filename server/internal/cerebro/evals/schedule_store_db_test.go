package evals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUpsertClaimAndMarkSchedule(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "scheduled-eval", 1)

	sched, err := store.UpsertSchedule(ctx, f.workspaceID, evalID, f.actorID, "@hourly", "", true)
	if err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}
	if sched.EvalID != evalID || !sched.Enabled || sched.NextRunAt == nil {
		t.Fatalf("unexpected schedule row: %+v", sched)
	}

	// next_run_at is the next hourly boundary (in the future). A claim anchored
	// two hours ahead sees it as due; anchored now it is not yet due.
	notYet, err := store.ClaimDueSchedules(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules (now): %v", err)
	}
	if scheduleInSet(notYet, sched.ID) {
		t.Fatal("schedule claimed before its next_run_at elapsed")
	}

	due, err := store.ClaimDueSchedules(ctx, time.Now().Add(2*time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules (future): %v", err)
	}
	if !scheduleInSet(due, sched.ID) {
		t.Fatal("due schedule was not claimed")
	}

	next := sched.NextRunAt.Add(time.Hour)
	if err := store.MarkScheduleRan(ctx, sched.ID, next); err != nil {
		t.Fatalf("MarkScheduleRan: %v", err)
	}

	after, err := store.ClaimDueSchedules(ctx, sched.NextRunAt.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules (after mark): %v", err)
	}
	if scheduleInSet(after, sched.ID) {
		t.Fatal("schedule still due after next_run_at was advanced")
	}

	// A disabled schedule is never claimed.
	if _, err := store.UpsertSchedule(ctx, f.workspaceID, evalID, f.actorID, "@hourly", "", false); err != nil {
		t.Fatalf("UpsertSchedule (disable): %v", err)
	}
	disabled, err := store.ClaimDueSchedules(ctx, time.Now().Add(48*time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules (disabled): %v", err)
	}
	if scheduleInSet(disabled, sched.ID) {
		t.Fatal("disabled schedule was claimed")
	}
}

func scheduleInSet(set []EvalSchedule, id uuid.UUID) bool {
	for _, s := range set {
		if s.ID == id {
			return true
		}
	}
	return false
}
