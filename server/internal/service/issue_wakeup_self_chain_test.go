package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupSelfContinuation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reset      string
		reply      bool
		event      bool
		deleteRule bool
		updateRule bool
		condition  string
		wantRefuse bool
	}{
		{name: "refuses ninth fresh one shot", wantRefuse: true},
		{name: "refuses unfiltered event continuation", event: true, wantRefuse: true},
		{name: "member comment resets chain", reset: "comment"},
		{name: "member created wakeup resets chain", reset: "wakeup"},
		{name: "member wake now resets chain", reset: "manual"},
		{name: "member filtered reply remains allowed", reply: true},
		{name: "deleted consumed rule preserves ancestry", deleteRule: true, wantRefuse: true},
		{name: "agent update preserves member created parent", reset: "wakeup", updateRule: true},
		{name: "member satisfied condition resets chain", condition: "member"},
		{name: "agent satisfied condition keeps chain", condition: "agent", wantRefuse: true},
		{name: "platform satisfied condition keeps chain", condition: "system", wantRefuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			ctx := context.Background()
			member := parseTestUUID(t, f.UserID)
			f.Exec(t, "UPDATE issue SET status='blocked' WHERE id=$1", issue)
			source := parseTestUUID(t, f.Task(t, agent, testutil.Cols{
				"issue_id": issue, "status": "completed",
				"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')"),
			}))

			// Each link is a new rule that really fires once and starts a run.
			// Eight creations must succeed before the ninth is considered.
			for link := 0; link < 8; link++ {
				in := WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "Check the blocked dependency"}
				creator := source
				if link == 4 {
					switch tc.reset {
					case "wakeup":
						creator = pgtype.UUID{}
					case "comment":
						in.Kind, in.AfterSeconds, in.EventTypes = "event", 0, []string{"comment.created"}
					}
				}
				if link == 7 && tc.condition != "" {
					in.Kind, in.AfterSeconds = "event", 0
					in.Condition = condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})
				}
				w, err := s.Create(ctx, issue, member, creator, in)
				if err != nil {
					t.Fatalf("link %d: %v", link+1, err)
				}
				switch {
				case link == 7 && tc.condition != "":
					actorID, actorSource := f.UserID, ""
					if tc.condition == "agent" {
						other := f.privateAgentOwnedBy(t, f.UserID, "condition-writer")
						actorID = other
						actorSource = f.Task(t, other, testutil.Cols{
							"issue_id": issue, "status": "completed",
							"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + other + "')"),
						})
					}
					if tc.condition == "system" {
						actorID = ""
					}
					tx, err := f.Pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
							t.Error(err)
						}
					}()
					if _, err = tx.Exec(ctx, "SELECT set_config('multica.actor_type',$1,true),set_config('multica.actor_id',$2,true),set_config('multica.source_task_id',$3,true)", tc.condition, actorID, actorSource); err != nil {
						t.Fatal(err)
					}
					if _, err = f.q.WithTx(tx).UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue, WorkspaceID: parseTestUUID(t, f.WorkspaceID), Status: "in_review"}); err != nil {
						t.Fatal(err)
					}
					if err = tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				case link == 4 && tc.reset == "comment":
					f.Comment(t, util.UUIDToString(issue), "Human provided new information")
				case link == 4 && tc.reset == "manual":
					if err = s.Trigger(ctx, issue, w.ID, WakeupTriggerInput{MemberID: member, ActorType: "member", ActorID: member}); err != nil {
						t.Fatal(err)
					}
				default:
					f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
				}
				wakeDispatch(t, s, w)
				fired, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
				if err != nil {
					t.Fatal(err)
				}
				if !fired.LastTaskID.Valid || fired.FireCount != 1 {
					t.Fatalf("link %d did not start exactly one run: task=%s fires=%d", link+1, util.UUIDToString(fired.LastTaskID), fired.FireCount)
				}
				source = fired.LastTaskID
				f.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", source)
				if link == 4 && tc.updateRule {
					if _, err = s.Save(ctx, issue, member, source, w.ID, in); err != nil {
						t.Fatal(err)
					}
					// Management re-enables the old rule, but this test only
					// continues the already-started run.
					if _, err = s.Disable(ctx, issue, w.ID, member); err != nil {
						t.Fatal(err)
					}
				}
				if link == 7 && tc.deleteRule {
					if err = s.Delete(ctx, issue, w.ID, member); err != nil {
						t.Fatal(err)
					}
				}
				f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", source)
			}

			in := WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "Check again"}
			if tc.reply || tc.event {
				in.Kind, in.AfterSeconds, in.EventTypes = "event", 0, []string{"comment.created"}
			}
			if tc.reply {
				in.FilterActorType, in.FilterActorID = "member", f.UserID
			}
			before := f.Count(t, "SELECT count(*) FROM issue_wakeup WHERE issue_id=$1", issue)
			w, err := s.Create(ctx, issue, member, source, in)
			if tc.wantRefuse {
				if !errors.Is(err, ErrWakeupInput) {
					t.Fatalf("ninth agent-only continuation must be refused with ErrWakeupInput, got %v", err)
				}
				if !strings.Contains(err.Error(), "human") {
					t.Fatalf("refusal must tell the agent to escalate to a human: %v", err)
				}
				if after := f.Count(t, "SELECT count(*) FROM issue_wakeup WHERE issue_id=$1", issue); after != before {
					t.Fatalf("refused creation persisted a rule: before=%d after=%d", before, after)
				}
				return
			}
			if err != nil {
				t.Fatalf("human reset or reply wait must allow creation: %v", err)
			}
			if !w.ID.Valid || !w.Enabled {
				t.Fatal("allowed continuation was not saved and enabled")
			}
		})
	}
}
