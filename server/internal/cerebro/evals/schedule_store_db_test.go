package evals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUpsertClaimAndMarkSchedule(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	enableEvalFeature(t, f.workspaceID)
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
	loaded, err := store.GetSchedule(ctx, f.workspaceID, evalID)
	if err != nil || loaded.ID != sched.ID {
		t.Fatalf("GetSchedule: row=%+v err=%v", loaded, err)
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
	leasingSweep, err := store.ClaimDueSchedules(ctx, time.Now().Add(2*time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules (leased): %v", err)
	}
	if scheduleInSet(leasingSweep, sched.ID) {
		t.Fatal("leased schedule was claimed by a second sweeper")
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
	if err := store.DeleteSchedule(ctx, f.workspaceID, evalID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	if _, err := store.GetSchedule(ctx, f.workspaceID, evalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSchedule after delete = %v, want ErrNotFound", err)
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
