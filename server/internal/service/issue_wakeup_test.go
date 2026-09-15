package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func wakeFixture(t *testing.T) (principalFixture, *IssueWakeupService, pgtype.UUID, string) {
	t.Helper()
	f, owner := newPrincipalFixture(t)
	if err := f.q.SeedIssueStatusEntries(context.Background(), parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	issue := f.Issue(t, "Wakeup test")
	agent := f.privateAgentOwnedBy(t, owner, "wake")
	// Service-created data is cleaned before fixture owners, including receipts.
	f.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	return f, &IssueWakeupService{Tasks: f.svc.TaskSvc}, parseTestUUID(t, issue), agent
}
func parseTestUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, e := util.ParseUUID(s)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func wakeCreate(t *testing.T, f principalFixture, s *IssueWakeupService, issue pgtype.UUID, in WakeupInput) db.IssueWakeup {
	t.Helper()
	w, e := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, in)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func wakeDispatch(t *testing.T, s *IssueWakeupService, w db.IssueWakeup) {
	t.Helper()
	if e := s.dispatch(context.Background(), w); e != nil {
		t.Fatal(e)
	}
}

func TestIssueWakeupEventAtomicOnceAndIndependentInputs(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review latest changes"})
	// A source rollback cannot leave a notification behind.
	tx, e := f.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(ctx, "INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'rollback','comment')", issue, f.WorkspaceID, f.UserID)
	if e != nil {
		t.Fatal(e)
	}
	_ = tx.Rollback(ctx)
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 0 {
		t.Fatalf("rollback left %d receipts", n)
	}
	comment := f.Comment(t, util.UUIDToString(issue), "source")
	// Existing assign task must coexist and must not absorb the wakeup.
	ordinary := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	wakeDispatch(t, s, w)
	wakeDispatch(t, s, w)
	got, e := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if e != nil {
		t.Fatal(e)
	}
	if got.Enabled || !got.LastTaskID.Valid {
		t.Fatalf("not consumed: %+v", got)
	}
	task, e := f.q.GetAgentTask(ctx, got.LastTaskID)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(task.HandoffNote.String, comment) || task.OriginatorUserID != parseTestUUID(t, f.UserID) {
		t.Fatal("missing input or principal")
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 1 {
		t.Fatalf("duplicate runs %d", n)
	}
	_, e = s.Disable(ctx, issue, w.ID, parseTestUUID(t, f.UserID))
	if e != nil {
		t.Fatal(e)
	}
	var status string
	f.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", ordinary).Scan(&status)
	if status != "queued" {
		t.Fatal("disable changed assign task")
	}
	f.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", task.ID).Scan(&status)
	if status != "cancelled" {
		t.Fatal("wakeup not withdrawn")
	}
}

func TestIssueWakeupTerminalStateAndScope(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	source := f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "completed", "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.completed"}, FilterTaskID: source, Instruction: "inspect output"})
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 1 {
		t.Fatal("missed already-terminal run")
	}
	wakeDispatch(t, s, w)
	otherIssue := f.Issue(t, "Other issue")
	_, err := s.Create(context.Background(), parseTestUUID(t, otherIssue), parseTestUUID(t, f.UserID), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.completed"}, FilterTaskID: source, Instruction: "invalid scope"})
	if !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("scope accepted: %v", err)
	}
}

func TestIssueWakeupContinuousSelfLoopAndClose(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created", "task.completed"}, Instruction: "check"})
	f.Comment(t, util.UUIDToString(issue), "one")
	wakeDispatch(t, s, w)
	got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	first := got.LastTaskID
	f.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", first)
	f.Comment(t, util.UUIDToString(issue), "self", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": first})
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatal("self comment reawakened")
	}
	f.Comment(t, util.UUIDToString(issue), "two")
	wakeDispatch(t, s, w)
	f.Comment(t, util.UUIDToString(issue), "three")
	wakeDispatch(t, s, w)
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND status='queued'", util.UUIDToString(w.ID)); n != 1 {
		t.Fatalf("pending backlog: %d", n)
	}
	f.Insert(t, "issue_status", testutil.Cols{"workspace_id": f.WorkspaceID, "key": "finished", "name": "Finished", "category": "done", "color": "#000000"})
	f.Exec(t, "UPDATE issue SET status='finished' WHERE id=$1", issue)
	got, _ = f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if got.Enabled || !got.DisabledAt.Valid {
		t.Fatal("custom end state did not disable")
	}
	task, _ := f.q.GetAgentTask(ctx, first)
	if task.Status != "running" {
		t.Fatal("close stopped active run")
	}
	f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", issue)
	got, _ = f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if got.Enabled {
		t.Fatal("reopen enabled old subscription")
	}
}

func TestIssueWakeupTimeRecoveryUpdateAndRevocation(t *testing.T) {
	for _, kind := range []string{"at", "every", "cron"} {
		t.Run(kind, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			ctx := context.Background()
			in := WakeupInput{AgentID: agent, Kind: kind, Instruction: "check CI"}
			switch kind {
			case "at":
				in.AfterSeconds = 600
			case "every":
				in.IntervalSeconds = 3600
			case "cron":
				in.CronExpression = "0 * * * *"
				in.Timezone = "Asia/Shanghai"
			}
			w := wakeCreate(t, f, s, issue, in)
			f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '3 hours' WHERE id=$1", w.ID)
			f.Exec(t, "UPDATE agent_runtime SET status='offline' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)", agent)
			wakeDispatch(t, s, w)
			wakeDispatch(t, s, w)
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 1 {
				t.Fatalf("offline catchup creates %d runs", n)
			}
			// Replacing the configuration withdraws the old queued input.
			updated, e := s.Save(ctx, issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, w.ID, in)
			if e != nil {
				t.Fatal(e)
			}
			if updated.Revision != 2 {
				t.Fatal("revision missing")
			}
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND status='queued'", util.UUIDToString(w.ID)); n != 0 {
				t.Fatal("stale revision still queued")
			}
			f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
			f.Exec(t, "DELETE FROM member WHERE user_id=$1 AND workspace_id=$2", f.UserID, f.WorkspaceID)
			wakeDispatch(t, s, updated)
			got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
			if got.Enabled || !got.LastError.Valid {
				t.Fatal("permission revoked but still enabled")
			}
		})
	}
}

func TestIssueWakeupValidate(t *testing.T) {
	s := IssueWakeupService{}
	for _, in := range []WakeupInput{{Kind: "event", Instruction: "x", EventTypes: []string{"review_ready"}}, {Kind: "every", Instruction: "x", IntervalSeconds: 1}, {Kind: "cron", Instruction: "x", CronExpression: "bad"}, {Kind: "at", Instruction: "x", AfterSeconds: -1}} {
		if _, err := s.Validate(&in, time.Now()); !errors.Is(err, ErrWakeupInput) {
			t.Errorf("accepted %+v", in)
		}
	}
}

func TestIssueWakeupStatusSourceAndClaimAuthority(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"issue.status_changed"}, Instruction: "check state"})
	f.Exec(t, "UPDATE issue SET status='in_progress',revision=revision+1 WHERE id=$1", issue)
	wakeDispatch(t, s, w)
	got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	_, err := f.q.UpdateIssue(ctx, db.UpdateIssueParams{ID: issue, Status: pgtype.Text{String: "in_review", Valid: true}, SourceTaskID: got.LastTaskID})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatal("self status update created a loop")
	}
	task, _ := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err = s.CheckClaim(ctx, task); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "DELETE FROM member WHERE user_id=$1 AND workspace_id=$2", f.UserID, f.WorkspaceID)
	if !errors.Is(s.CheckClaim(ctx, task), ErrWakeupForbidden) {
		t.Fatal("revoked member still claimable")
	}
}

func TestIssueWakeupRetryKeepsPromptAndSchedulingScope(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	parent := f.Comment(t, util.UUIDToString(issue), "original request")
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, ParentCommentID: parent, Instruction: "specific wakeup instruction"})
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	wakeDispatch(t, s, w)
	got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	task, _ := f.q.GetAgentTask(ctx, got.LastTaskID)
	if task.CommentThreadID != w.ID || util.UUIDToString(task.TriggerCommentID) != parent {
		t.Fatal("delivery thread and scheduling scope confused")
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1", task.ID)
	child, err := f.q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.HandoffNote != task.HandoffNote || child.CommentThreadID != w.ID {
		t.Fatal("retry lost wakeup prompt or scope")
	}
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", child.ID)
}

func TestIssueWakeupConcurrentDispatchIsIdempotent(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "once"})
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs <- s.dispatch(ctx, w)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 1 {
		t.Fatalf("created %d runs", n)
	}
}
