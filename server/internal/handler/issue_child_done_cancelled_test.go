package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStageProgressSummarySeparatesCancelledFromDone(t *testing.T) {
	children := []db.Issue{
		child(1, "done"),
		child(2, "cancelled"), child(2, "cancelled"),
		child(3, "backlog"),
	}

	summary, next := stageProgressSummary(children, 2, literalChildStatus)
	want := "Stage 1: 1/1 done; Stage 2: 0/2 done, 2 cancelled; Stage 3: 0/1 done (next)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if next != 3 {
		t.Fatalf("nextStage = %d, want 3", next)
	}
}

func TestResolvedChildStatusesKeepCanonicalCancelled(t *testing.T) {
	doneID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	cancelledID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	children := []db.Issue{
		{ID: doneID, Status: "approved", Stage: pgtype.Int4{Int32: 1, Valid: true}},
		{ID: cancelledID, Status: "wont_do", Stage: pgtype.Int4{Int32: 1, Valid: true}},
	}

	statuses, err := resolveChildStatuses(children, func(c db.Issue) (string, error) {
		switch c.Status {
		case "approved":
			return "done", nil
		case "wont_do":
			return "cancelled", nil
		default:
			return c.Status, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses.status(children[1]); got != "cancelled" {
		t.Fatalf("canonical status = %q, want cancelled", got)
	}
	if !statuses.isTerminal(children[1]) {
		t.Fatal("canonical cancelled status must still close the barrier")
	}

	summary, _ := stageProgressSummary(children, 1, statuses.status)
	if want := "Stage 1: 1/2 done, 1 cancelled"; summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestBatchClosedScopeHasCancelled(t *testing.T) {
	stage2Cancelled := child(2, "cancelled")
	stage2Cancelled.ID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	stage7Done := child(7, "done")
	stage7Done.ID = pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	stage20Cancelled := child(20, "cancelled")
	stage20Cancelled.ID = pgtype.UUID{Bytes: [16]byte{20}, Valid: true}
	children := []db.Issue{stage2Cancelled, stage7Done, stage20Cancelled}
	statusOf := func(c db.Issue) string { return c.Status }

	t.Run("lower stage cancelled in the same batch warns", func(t *testing.T) {
		if !batchClosedScopeHasCancelled(children, []db.Issue{stage2Cancelled, stage7Done}, 7, statusOf) {
			t.Fatal("expected cancellation in a lower stage closed by the same batch to be reported")
		}
	})

	t.Run("later unopened stage cancellation is ignored", func(t *testing.T) {
		if batchClosedScopeHasCancelled(children, []db.Issue{stage7Done, stage20Cancelled}, 7, statusOf) {
			t.Fatal("a cancellation above the highest closed stage must not affect the current advance decision")
		}
	})

	t.Run("historical lower-stage cancellation is not repeated", func(t *testing.T) {
		if batchClosedScopeHasCancelled(children, []db.Issue{stage7Done}, 7, statusOf) {
			t.Fatal("a lower-stage cancellation outside this batch must not re-trigger the warning")
		}
	})
}

func TestStageAdvanceInstructionWarnsOnCancelledWork(t *testing.T) {
	got := stageAdvanceInstruction(3, "parent-id", true)
	for _, want := range []string{
		"Stage 3 is next",
		"confirm that the cancelled work is not a dependency",
		"do not promote or create the next stage yet",
		"post a comment to confirm first",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("instruction missing %q: %s", want, got)
		}
	}
}
