package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupExpiryValidation(t *testing.T) {
	s := IssueWakeupService{}
	now := time.Now()
	past, far := now.Add(-time.Minute), now.Add(400*24*time.Hour)
	soon := now.Add(time.Hour)
	for name, in := range map[string]WakeupInput{
		"single time takes no deadline": {Kind: "at", ExpiresInSeconds: 3600},
		"both forms":                    {Kind: "event", ExpiresAt: &soon, ExpiresInSeconds: 3600},
		"timeout without deadline":      {Kind: "event", OnTimeout: "wake"},
		"time rule cannot wake":         {Kind: "every", ExpiresInSeconds: 3600, OnTimeout: "wake"},
		"unknown timeout":               {Kind: "event", ExpiresInSeconds: 3600, OnTimeout: "retry"},
		"too short":                     {Kind: "event", ExpiresInSeconds: 30},
		"past deadline":                 {Kind: "cron", ExpiresAt: &past},
		"beyond a year":                 {Kind: "cron", ExpiresAt: &far},
	} {
		in := in
		if _, _, err := s.Expiry(&in, now); !errors.Is(err, ErrWakeupInput) {
			t.Errorf("%s: accepted %+v", name, in)
		}
	}
	in := WakeupInput{Kind: "event", ExpiresInSeconds: 3600}
	at, seconds, err := s.Expiry(&in, now)
	if err != nil || !at.Time.Equal(now.Add(time.Hour)) || seconds.Int64 != 3600 || in.OnTimeout != "end" {
		t.Fatalf("relative wait: %v %v %v %q", at, seconds, err, in.OnTimeout)
	}
	in = WakeupInput{Kind: "every"}
	if at, seconds, err = s.Expiry(&in, now); err != nil || at.Valid || seconds.Valid {
		t.Fatal("rules without a deadline stay open-ended for existing clients")
	}
}

// A deadline that passes without a subscribed event ends the rule. With
// on_timeout=wake the target runs once with the timeout as its trigger fact;
// the rule reads as timed out, not as turned off, so that run stays claimable.
func TestIssueWakeupTimeoutWakesOnceAndEnds(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "follow up", ExpiresInSeconds: 3600, OnTimeout: "wake"})
	if !w.ExpiresAt.Valid || w.ExpirySeconds.Int64 != 3600 || w.OnTimeout.String != "wake" {
		t.Fatalf("expiry not stored: %+v", w)
	}
	wakeDispatch(t, s, w)
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 0 {
		t.Fatal("dispatched before the deadline")
	}
	f.Exec(t, "UPDATE issue_wakeup SET expires_at=now()-interval '1 second' WHERE id=$1", w.ID)
	ready, err := f.q.ListReadyWakeups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range ready {
		found = found || r.ID == w.ID
	}
	if !found {
		t.Fatal("expired rule is not a scheduler candidate")
	}
	wakeDispatch(t, s, w)
	wakeDispatch(t, s, w)
	var note string
	if err = f.Pool.QueryRow(ctx, "SELECT handoff_note FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "wakeup.timeout") {
		t.Fatalf("timeout fact missing from the prompt: %s", note)
	}
	got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if got.Enabled || !got.TimedOutAt.Valid || got.DisabledAt.Valid {
		t.Fatalf("timed-out rule state: %+v", got)
	}
	// Later events do not reach a timed-out rule.
	f.Exec(t, "INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'late','comment')", issue, f.WorkspaceID, f.UserID)
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatal("timed-out rule captured a later event")
	}

	// Enabling again restarts the relative wait from now.
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID))
	again, err := s.Enable(ctx, issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: got.Revision, Rearm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Enabled || again.TimedOutAt.Valid || !again.ExpiresAt.Time.After(time.Now().Add(59*time.Minute)) {
		t.Fatalf("rearm kept the old deadline: %+v", again)
	}
}

func TestIssueWakeupTimeoutEndsQuietly(t *testing.T) {
	for _, kind := range []string{"event", "every"} {
		t.Run(kind, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			ctx := context.Background()
			in := WakeupInput{AgentID: agent, Kind: kind, Instruction: "check", ExpiresInSeconds: 7200}
			if kind == "event" {
				in.EventTypes = []string{"comment.created"}
			} else {
				in.IntervalSeconds = 3600
			}
			w := wakeCreate(t, f, s, issue, in)
			f.Exec(t, "UPDATE issue_wakeup SET expires_at=now()-interval '1 second' WHERE id=$1", w.ID)
			wakeDispatch(t, s, w)
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 0 {
				t.Fatal("on_timeout=end must not start a run")
			}
			got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
			if got.Enabled || !got.TimedOutAt.Valid || got.NextFireAt.Valid {
				t.Fatalf("rule did not end: %+v", got)
			}
		})
	}
}

// An absolute deadline that has passed cannot be re-enabled; the rule must be
// replaced with a new end first.
func TestIssueWakeupPastAbsoluteDeadlineCannotBeEnabled(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	end := time.Now().Add(48 * time.Hour)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "cron", CronExpression: "0 9 * * *", Timezone: "Asia/Shanghai", Instruction: "daily check", ExpiresAt: &end})
	if w.ExpirySeconds.Valid {
		t.Fatal("absolute deadline stored as a relative wait")
	}
	f.Exec(t, "UPDATE issue_wakeup SET expires_at=now()-interval '1 second' WHERE id=$1", w.ID)
	wakeDispatch(t, s, w)
	got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if _, err := s.Enable(ctx, issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: got.Revision}); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("re-enabled past its deadline: %v", err)
	}
}
