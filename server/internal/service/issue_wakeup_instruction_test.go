package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestIssueWakeupInstructionPreservesStateAndPendingEvents(t *testing.T) {
	for _, kind := range []string{"event", "at", "every", "cron"} {
		t.Run(kind, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			ctx := context.Background()
			owner := parseTestUUID(t, f.UserID)
			input := WakeupInput{AgentID: agent, Kind: kind, Instruction: "original", Timezone: "UTC"}
			switch kind {
			case "event":
				input.EventTypes = []string{"comment.created"}
			case "at":
				input.AfterSeconds = 600
			case "every":
				input.IntervalSeconds = 600
			case "cron":
				input.CronExpression = "0 * * * *"
			}
			w := wakeCreate(t, f, s, issue, input)
			if kind == "event" {
				f.Comment(t, util.UUIDToString(issue), "captured before edit")
			}
			before, err := s.Tasks.Queries.LocklessWakeup(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			in := WakeupInstructionInput{Instruction: "new instructions", ExpectedInstruction: w.Instruction, Revision: w.Revision}
			if err = s.EditInstruction(ctx, issue, w.ID, owner, in); err != nil {
				t.Fatal(err)
			}
			after, err := s.Tasks.Queries.LocklessWakeup(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			normalized := after
			normalized.Instruction = before.Instruction
			normalized.UpdatedAt = before.UpdatedAt
			if !reflect.DeepEqual(before, normalized) {
				t.Fatalf("editing altered subscription: before=%+v after=%+v", before, after)
			}
			if err = s.EditInstruction(ctx, issue, w.ID, owner, in); !errors.Is(err, ErrWakeupConflict) {
				t.Fatalf("stale edit: %v", err)
			}
			if kind == "event" {
				wakeDispatch(t, s, after)
				var note string
				if err = f.Pool.QueryRow(ctx, "SELECT handoff_note FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_id'=$2", issue, util.UUIDToString(w.ID)).Scan(&note); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(note, "new instructions") {
					t.Fatal("pending event lost or dispatched old instructions")
				}
			}
			if _, err = s.Disable(ctx, issue, w.ID, owner); err != nil {
				t.Fatal(err)
			}
			in.ExpectedInstruction = in.Instruction
			in.Instruction = "saved while off"
			if err = s.EditInstruction(ctx, issue, w.ID, owner, in); err != nil {
				t.Fatal(err)
			}
			off, err := s.Tasks.Queries.LocklessWakeup(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if off.Enabled || !off.DisabledAt.Valid || off.NextFireAt != after.NextFireAt {
				t.Fatal("edit rearmed disabled rule")
			}
		})
	}
}

func TestIssueWakeupInstructionPreservesQueuedRunAndChecksScope(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	owner := parseTestUUID(t, f.UserID)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "queued instruction"})
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	wakeDispatch(t, s, w)
	in := WakeupInstructionInput{Instruction: "future instruction", ExpectedInstruction: w.Instruction, Revision: w.Revision}
	if err := s.EditInstruction(ctx, issue, w.ID, owner, in); err != nil {
		t.Fatal(err)
	}
	var note string
	if err := f.Pool.QueryRow(ctx, "SELECT handoff_note FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_id'=$2", issue, util.UUIDToString(w.ID)).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "queued instruction") || strings.Contains(note, "future instruction") {
		t.Fatal("edit rewrote queued run")
	}
	current, err := s.Tasks.Queries.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Enabled || current.Revision != w.Revision {
		t.Fatal("edit rearmed consumed rule")
	}
	in.ExpectedInstruction = in.Instruction
	other := f.Issue(t, "other issue")
	if err = s.EditInstruction(ctx, parseTestUUID(t, other), w.ID, owner, in); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross issue: %v", err)
	}
	outsider := f.User(t, "outsider", "edit-outsider@multica.test")
	if err = s.EditInstruction(ctx, issue, w.ID, parseTestUUID(t, outsider), in); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("outsider: %v", err)
	}
	f.Member(t, f.WorkspaceID, outsider, "member")
	if err = s.EditInstruction(ctx, issue, w.ID, parseTestUUID(t, outsider), in); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("other member: %v", err)
	}
	for _, value := range []string{" ", strings.Repeat("中", 4001)} {
		in.Instruction = value
		if err = s.EditInstruction(ctx, issue, w.ID, owner, in); !errors.Is(err, ErrWakeupInput) {
			t.Fatalf("invalid text accepted: %v", err)
		}
	}
	// A revision change fences edits even when the old prompt remains the same.
	in.Instruction = "valid"
	in.Revision++
	if err = s.EditInstruction(ctx, issue, w.ID, owner, in); !errors.Is(err, ErrWakeupConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	// Editing requires current invoke rights; it cannot borrow the rule owner's rights.
	f.Exec(t, "UPDATE agent SET archived_at=$2 WHERE id=$1", agent, time.Now())
	in.Revision = w.Revision
	if err = s.EditInstruction(ctx, issue, w.ID, owner, in); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("archived target: %v", err)
	}
}
