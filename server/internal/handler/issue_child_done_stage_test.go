package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// child builds a sibling row with the given stage (0 = unstaged/NULL) and
// status, the only two fields the stage-barrier logic reads.
func child(stage int32, status string) db.Issue {
	c := db.Issue{Status: status}
	if stage != 0 {
		c.Stage = pgtype.Int4{Int32: stage, Valid: true}
	}
	return c
}

// withStatuses stamps a distinct ID on every child (child() leaves the zero
// UUID) and returns the snapshot resolveChildStatuses would build from their
// literal statuses, which is what the summary helpers consume.
func withStatuses(children []db.Issue) ([]db.Issue, childStatuses) {
	statuses := make(childStatuses, len(children))
	for i := range children {
		children[i].ID = pgtype.UUID{Bytes: [16]byte{byte(i + 1), byte((i + 1) >> 8)}, Valid: true}
		statuses[children[i].ID] = children[i].Status
	}
	return children, statuses
}

func TestStageBarrierClosed_Unstaged(t *testing.T) {
	tests := []struct {
		name     string
		children []db.Issue
		want     bool
	}{
		{
			name:     "last child still leaves a sibling open",
			children: []db.Issue{child(0, "done"), child(0, "in_progress")},
			want:     false,
		},
		{
			name:     "every child terminal closes the single implicit stage",
			children: []db.Issue{child(0, "done"), child(0, "done")},
			want:     true,
		},
		{
			name:     "a backlog sibling holds the barrier open (no surprise cascade)",
			children: []db.Issue{child(0, "done"), child(0, "backlog")},
			want:     false,
		},
		{
			name:     "cancelled counts as terminal",
			children: []db.Issue{child(0, "done"), child(0, "cancelled")},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// completed is one of the terminal children; identity doesn't matter
			// for the unstaged path.
			if got := stageBarrierClosed(tt.children, child(0, "done"), literalTerminalChild); got != tt.want {
				t.Fatalf("stageBarrierClosed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStageBarrierClosed_Staged(t *testing.T) {
	// Three stages: 1 has two children, 2 has two, 3 has one.
	t.Run("stage 1 not fully done does not fire", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "in_progress"),
			child(2, "backlog"), child(2, "backlog"),
			child(3, "backlog"),
		}
		if stageBarrierClosed(children, child(1, "done"), literalTerminalChild) {
			t.Fatal("expected barrier not closed while stage 1 has an open child")
		}
	})

	t.Run("closing stage 1 fires even though later stages are parked", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "backlog"), child(2, "backlog"),
			child(3, "backlog"),
		}
		if !stageBarrierClosed(children, child(1, "done"), literalTerminalChild) {
			t.Fatal("expected stage 1 barrier to close")
		}
	})

	t.Run("closing stage 2 fires when stages 1 and 2 are terminal", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "done"), child(2, "done"),
			child(3, "backlog"),
		}
		if !stageBarrierClosed(children, child(2, "done"), literalTerminalChild) {
			t.Fatal("expected stage 2 barrier to close")
		}
	})

	t.Run("final stage closes once its child finishes", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "done"), child(2, "done"),
			child(3, "done"),
		}
		if !stageBarrierClosed(children, child(3, "done"), literalTerminalChild) {
			t.Fatal("expected final stage barrier to close")
		}
	})
}

func TestStageProgressSummary(t *testing.T) {
	children, statuses := withStatuses([]db.Issue{
		child(1, "done"), child(1, "done"), child(1, "done"),
		child(2, "backlog"), child(2, "backlog"), child(2, "backlog"), child(2, "backlog"),
		child(3, "backlog"), child(3, "backlog"),
	})
	summary, next := stageProgressSummary(children, 1, statuses)
	want := "Stage 1: 3/3 done; Stage 2: 0/4 done (next); Stage 3: 0/2 done"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if next != 2 {
		t.Fatalf("nextStage = %d, want 2", next)
	}
}

func TestStageProgressSummary_FinalStageNoNext(t *testing.T) {
	children, statuses := withStatuses([]db.Issue{
		child(1, "done"), child(1, "done"),
		child(2, "done"),
	})
	_, next := stageProgressSummary(children, 2, statuses)
	if next != 0 {
		t.Fatalf("nextStage = %d, want 0 (no further stages)", next)
	}
}

func TestStageProgressSummary_SkipsUnstaged(t *testing.T) {
	// An unstaged child must not appear as "Stage 0" nor inflate any stage.
	children, statuses := withStatuses([]db.Issue{
		child(0, "backlog"), // unstaged — ignored
		child(1, "done"), child(1, "done"),
		child(2, "backlog"),
	})
	summary, next := stageProgressSummary(children, 1, statuses)
	want := "Stage 1: 2/2 done; Stage 2: 0/1 done (next)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if next != 2 {
		t.Fatalf("nextStage = %d, want 2", next)
	}
}

// A cancelled sibling closes its stage but did not do its work, so it must
// not be counted as done (#8462). The clause is omitted at zero, which keeps
// the three summaries above byte-identical to their pre-#8462 wording.
func TestStageProgressSummary_CancelledIsNotDone(t *testing.T) {
	children, statuses := withStatuses([]db.Issue{
		child(1, "done"),
		child(2, "cancelled"), child(2, "cancelled"),
		child(3, "done"), child(3, "cancelled"), child(3, "todo"),
		child(4, "backlog"),
	})
	summary, next := stageProgressSummary(children, 2, statuses)
	want := "Stage 1: 1/1 done; Stage 2: 0/2 done, 2 cancelled; Stage 3: 1/3 done, 1 cancelled (next); Stage 4: 0/1 done"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	// Cancelled still closes a stage: stage 2 is fully terminal, so the next
	// open stage is 3, exactly as before.
	if next != 3 {
		t.Fatalf("nextStage = %d, want 3", next)
	}
}

// A custom status in the cancelled category ("Won't Do", "Duplicate")
// resolves to the canonical key before the summary sees it, so it folds into
// the cancelled counter and wording rather than being reported as done. The
// only reader of this comment is an agent whose vocabulary is the built-in
// one; the custom display name adds nothing to its decision (#8462).
func TestResolveChildStatuses_CustomCancelledCategoryReadsAsCancelled(t *testing.T) {
	children, _ := withStatuses([]db.Issue{child(1, "done"), child(1, "wont_do")})
	effective := func(c db.Issue) (string, error) {
		if c.Status == "wont_do" {
			return "cancelled", nil
		}
		return c.Status, nil
	}
	statuses, err := resolveChildStatuses(children, effective)
	if err != nil {
		t.Fatalf("resolveChildStatuses: %v", err)
	}
	if !statuses.terminal(children[1]) || !statuses.cancelled(children[1]) {
		t.Fatalf("custom cancelled-category child must read as terminal and cancelled, got terminal=%v cancelled=%v",
			statuses.terminal(children[1]), statuses.cancelled(children[1]))
	}
	if statuses.cancelled(children[0]) {
		t.Fatal("a done child must not read as cancelled")
	}
	summary, _ := stageProgressSummary(children, 1, statuses)
	if want := "Stage 1: 1/2 done, 1 cancelled"; summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

// stageAdvanceInstruction must point at a known next stage when one exists,
// and — the core of MUL-4062 — must NOT assert finality when no later stage
// exists yet, because a lazily-created intermediate stage reaches nextStage==0
// exactly like a true final stage does.
func TestStageAdvanceInstruction(t *testing.T) {
	const parentID = "parent-uuid"

	t.Run("a known next stage points the leader at it", func(t *testing.T) {
		got := stageAdvanceInstruction(3, parentID, 0)
		if !strings.Contains(got, "Stage 3 is next") {
			t.Fatalf("expected next-stage instruction, got %q", got)
		}
		if strings.Contains(got, "cancelled") {
			t.Fatalf("no cancellation must add no cancellation note, got %q", got)
		}
	})

	// #8462: a stage closed by cancellation hands the "is the next stage still
	// valid" decision back, with the count, instead of the server deciding.
	t.Run("cancelled sub-issues in the closed stage ask for confirmation before promoting", func(t *testing.T) {
		got := stageAdvanceInstruction(3, parentID, 2)
		for _, want := range []string{"Stage 3 is next", "2 sub-issues cancelled", "Stage 3 depends on", "post a comment asking for confirmation instead of promoting"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in %q", want, got)
			}
		}
	})

	t.Run("no created next stage with a cancellation says closed, not completed", func(t *testing.T) {
		got := stageAdvanceInstruction(0, parentID, 1)
		if !strings.HasPrefix(got, " Closing this stage does not mean the whole issue is done.") {
			t.Fatalf("expected the closing wording, got %q", got)
		}
		for _, want := range []string{"1 sub-issue cancelled", "before creating the next stage or marking the parent ready for review", "post a comment asking for confirmation first"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in %q", want, got)
			}
		}
	})

	t.Run("no created next stage does not assert finality", func(t *testing.T) {
		got := stageAdvanceInstruction(0, parentID, 0)
		// Regression guard for MUL-4062: an intermediate stage in a lazily
		// created workflow also reaches nextStage==0, so the message must not
		// claim this was definitively the final stage.
		if strings.Contains(got, "This was the final stage") {
			t.Fatalf("must not assert finality when the workflow shape is unknown, got %q", got)
		}
		// It must make clear that finishing the stage != the whole issue is
		// done, and hand both paths (wrap up / create the next stage) to the
		// leader. The explicit in_review command marks the wrap-up moment;
		// the write itself is authorized by the standing status-ownership
		// grant (MUL-6300), not by this ask.
		if !strings.Contains(got, "does not mean the whole issue is done") {
			t.Fatalf("expected stage-done != issue-done framing, got %q", got)
		}
		if !strings.Contains(got, "next stage") {
			t.Fatalf("expected create-next-stage guidance, got %q", got)
		}
		if !strings.Contains(got, "multica issue status "+parentID+" in_review") {
			t.Fatalf("expected explicit in_review instruction for confirmed completion, got %q", got)
		}
	})
}

// A stage can close because its last open child is *cancelled*, not only
// done — a cancelled sibling never finishes, so it must not hold the stage open.
func TestStageBarrierClosed_CancelledClosesStage(t *testing.T) {
	t.Run("staged: cancelling the last open child closes the stage", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "cancelled"),
			child(2, "backlog"),
		}
		if !stageBarrierClosed(children, child(1, "cancelled"), literalTerminalChild) {
			t.Fatal("expected the stage to close when its last open child is cancelled")
		}
	})
	t.Run("unstaged: cancel of the last open child closes the implicit stage", func(t *testing.T) {
		children := []db.Issue{child(0, "done"), child(0, "cancelled")}
		if !stageBarrierClosed(children, child(0, "cancelled"), literalTerminalChild) {
			t.Fatal("expected the implicit stage to close on cancel")
		}
	})
}

// In a staged set, unstaged children do not participate in the frontier:
// they neither hold a stage open nor close anything on their own completion.
func TestStageBarrierClosed_UnstagedIgnoredInStagedSet(t *testing.T) {
	t.Run("a non-terminal unstaged child does not block stage 1", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(0, "backlog"), // unstaged, still open — must NOT block
		}
		if !stageBarrierClosed(children, child(1, "done"), literalTerminalChild) {
			t.Fatal("expected stage 1 to close; an unstaged child must not hold it open")
		}
	})
	t.Run("completing an unstaged child in a staged set closes nothing", func(t *testing.T) {
		children := []db.Issue{
			child(1, "backlog"),
			child(0, "done"), // the just-completed unstaged child
		}
		if stageBarrierClosed(children, child(0, "done"), literalTerminalChild) {
			t.Fatal("an unstaged child's completion must not fire a stage barrier")
		}
	})
}

// literalTerminalChild is the pre-MUL-6243 terminal test: it reads the status
// literal directly, with no catalog resolution. The stage-barrier logic these
// tests cover is pure and operates on CANONICAL statuses, so pinning it with a
// literal predicate keeps them testing the barrier itself rather than status
// resolution (which has its own coverage in issue_status_test.go).
func literalTerminalChild(c db.Issue) bool {
	return isTerminalChildStatus(c.Status)
}
