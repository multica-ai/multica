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
			if got := stageBarrierClosed(tt.children, child(0, "done")); got != tt.want {
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
		if stageBarrierClosed(children, child(1, "done")) {
			t.Fatal("expected barrier not closed while stage 1 has an open child")
		}
	})

	t.Run("closing stage 1 fires even though later stages are parked", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "backlog"), child(2, "backlog"),
			child(3, "backlog"),
		}
		if !stageBarrierClosed(children, child(1, "done")) {
			t.Fatal("expected stage 1 barrier to close")
		}
	})

	t.Run("closing stage 2 fires when stages 1 and 2 are terminal", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "done"), child(2, "done"),
			child(3, "backlog"),
		}
		if !stageBarrierClosed(children, child(2, "done")) {
			t.Fatal("expected stage 2 barrier to close")
		}
	})

	t.Run("final stage closes once its child finishes", func(t *testing.T) {
		children := []db.Issue{
			child(1, "done"), child(1, "done"),
			child(2, "done"), child(2, "done"),
			child(3, "done"),
		}
		if !stageBarrierClosed(children, child(3, "done")) {
			t.Fatal("expected final stage barrier to close")
		}
	})
}

func TestStageProgressSummary(t *testing.T) {
	children := []db.Issue{
		child(1, "done"), child(1, "done"), child(1, "done"),
		child(2, "backlog"), child(2, "backlog"), child(2, "backlog"), child(2, "backlog"),
		child(3, "backlog"), child(3, "backlog"),
	}
	summary, next := stageProgressSummary(children, 1)
	want := "Stage 1: 3/3 done; Stage 2: 0/4 done (next); Stage 3: 0/2 done"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if next != 2 {
		t.Fatalf("nextStage = %d, want 2", next)
	}
}

func TestStageProgressSummary_FinalStageNoNext(t *testing.T) {
	children := []db.Issue{
		child(1, "done"), child(1, "done"),
		child(2, "done"),
	}
	_, next := stageProgressSummary(children, 2)
	if next != 0 {
		t.Fatalf("nextStage = %d, want 0 (no further stages)", next)
	}
}

func TestStageProgressSummary_SkipsUnstaged(t *testing.T) {
	// An unstaged child must not appear as "Stage 0" nor inflate any stage.
	children := []db.Issue{
		child(0, "backlog"), // unstaged — ignored
		child(1, "done"), child(1, "done"),
		child(2, "backlog"),
	}
	summary, next := stageProgressSummary(children, 1)
	want := "Stage 1: 2/2 done; Stage 2: 0/1 done (next)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if next != 2 {
		t.Fatalf("nextStage = %d, want 2", next)
	}
}

// stageAdvanceInstruction must point at a known next stage when one exists,
// and — the core of MUL-4062 — must NOT assert finality when no later stage
// exists yet, because a lazily-created intermediate stage reaches nextStage==0
// exactly like a true final stage does.
func TestStageAdvanceInstruction(t *testing.T) {
	const parentID = "parent-uuid"

	t.Run("a known next stage points the leader at it", func(t *testing.T) {
		got := stageAdvanceInstruction(3, parentID)
		if !strings.Contains(got, "Stage 3 is next") {
			t.Fatalf("expected next-stage instruction, got %q", got)
		}
	})

	t.Run("no created next stage does not assert finality", func(t *testing.T) {
		got := stageAdvanceInstruction(0, parentID)
		// Regression guard for MUL-4062: an intermediate stage in a lazily
		// created workflow also reaches nextStage==0, so the message must not
		// claim this was definitively the final stage.
		if strings.Contains(got, "This was the final stage") {
			t.Fatalf("must not assert finality when the workflow shape is unknown, got %q", got)
		}
		// It must make clear that finishing the stage != the whole issue is
		// done, and hand both paths (wrap up / create the next stage) to the
		// leader.
		if !strings.Contains(got, "does not mean the whole issue is done") {
			t.Fatalf("expected stage-done != issue-done framing, got %q", got)
		}
		if !strings.Contains(got, "next stage") {
			t.Fatalf("expected create-next-stage guidance, got %q", got)
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
		if !stageBarrierClosed(children, child(1, "cancelled")) {
			t.Fatal("expected the stage to close when its last open child is cancelled")
		}
	})
	t.Run("unstaged: cancel of the last open child closes the implicit stage", func(t *testing.T) {
		children := []db.Issue{child(0, "done"), child(0, "cancelled")}
		if !stageBarrierClosed(children, child(0, "cancelled")) {
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
		if !stageBarrierClosed(children, child(1, "done")) {
			t.Fatal("expected stage 1 to close; an unstaged child must not hold it open")
		}
	})
	t.Run("completing an unstaged child in a staged set closes nothing", func(t *testing.T) {
		children := []db.Issue{
			child(1, "backlog"),
			child(0, "done"), // the just-completed unstaged child
		}
		if stageBarrierClosed(children, child(0, "done")) {
			t.Fatal("an unstaged child's completion must not fire a stage barrier")
		}
	})
}
