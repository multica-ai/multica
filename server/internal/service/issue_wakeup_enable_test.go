package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupEnablePreservesConfigAndFencesReplay(t *testing.T) {
	for _, kind := range []string{"event", "at", "every", "cron"} {
		t.Run(kind, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			ctx := context.Background()
			owner := parseTestUUID(t, f.UserID)
			parent := f.Comment(t, util.UUIDToString(issue), "delivery thread")
			in := WakeupInput{AgentID: agent, Kind: kind, Instruction: "keep my instruction", ParentCommentID: parent, Timezone: "Asia/Shanghai"}
			switch kind {
			case "event":
				in.EventTypes = []string{"task.completed"}
				in.FilterAgentID = agent
			case "at":
				in.AfterSeconds = 3600
			case "every":
				in.IntervalSeconds = 3600
			case "cron":
				in.CronExpression = "0 * * * *"
			}
			w := wakeCreate(t, f, s, issue, in)
			if _, err := s.Disable(ctx, issue, w.ID, owner); err != nil {
				t.Fatal(err)
			}
			// Missed recurring periods must not generate catch-up runs on enable.
			if kind == "every" || kind == "cron" {
				f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '3 hours' WHERE id=$1", w.ID)
			}
			restored, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if !restored.Enabled || restored.DisabledAt.Valid || restored.Revision != w.Revision+1 || restored.Instruction != w.Instruction || restored.ParentCommentID != w.ParentCommentID || restored.FilterAgentID != w.FilterAgentID || restored.Timezone != w.Timezone {
				t.Fatalf("changed configuration: %+v", restored)
			}
			if kind != "event" && !restored.NextFireAt.Time.After(time.Now()) {
				t.Fatal("restored timer catches up in the past")
			}
			if _, err = s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision}); !errors.Is(err, ErrWakeupConflict) {
				t.Fatalf("stale enable accepted: %v", err)
			}
			// Already-on toggle with current revision is an idempotent read.
			same, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: restored.Revision})
			if err != nil || same.Revision != restored.Revision || same.NextFireAt != restored.NextFireAt {
				t.Fatalf("duplicate toggled schedule: %v", err)
			}
		})
	}
}

func TestIssueWakeupEnableOneShotRequiresExplicitFutureRearm(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	owner := parseTestUUID(t, f.UserID)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "check"})
	if _, err := s.Disable(ctx, issue, w.ID, owner); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 minute' WHERE id=$1", w.ID)
	if _, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision}); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("expired toggle accepted: %v", err)
	}
	future := time.Now().Add(time.Hour)
	restored, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision, At: &future, Rearm: true})
	if err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	wakeDispatch(t, s, restored)
	if _, err = s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: restored.Revision, At: &future}); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("consumed toggle accepted: %v", err)
	}
	if _, err = s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: restored.Revision, At: &future, Rearm: true}); !errors.Is(err, ErrWakeupConflict) {
		t.Fatalf("active run rearmed: %v", err)
	}
	if _, err = s.Disable(ctx, issue, w.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: restored.Revision, At: &future, Rearm: true}); err != nil {
		t.Fatal(err)
	}
}

func TestIssueWakeupEnableChecksScopeAuthorityAndClosedState(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	owner := parseTestUUID(t, f.UserID)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.completed"}, Instruction: "check"})
	if _, err := s.Disable(ctx, issue, w.ID, owner); err != nil {
		t.Fatal(err)
	}
	input := WakeupEnableInput{Revision: w.Revision}
	outsider := parseTestUUID(t, f.member(t, "enable-outsider"))
	if _, err := s.Enable(ctx, issue, outsider, pgtype.UUID{}, w.ID, input); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("borrowed permission: %v", err)
	}
	other := parseTestUUID(t, f.Issue(t, "other"))
	if _, err := s.Enable(ctx, other, owner, pgtype.UUID{}, w.ID, input); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("wrong scope: %v", err)
	}
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", issue)
	if _, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, input); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("closed enable: %v", err)
	}
	f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", issue)
	current, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil || current.Enabled {
		t.Fatal("reopen automatically enabled")
	}
	if _, err = s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, input); err != nil {
		t.Fatal(err)
	}
}
