package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupMemberFilterPreservesOnceUntilMatchingComment(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	other := f.User(t, "monitored member", "wakeup-member@multica.test")
	f.Member(t, f.WorkspaceID, other, "member")
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, FilterActorType: "member", FilterActorID: other, Instruction: "continue after member reply"})
	f.Comment(t, util.UUIDToString(issue), "another member")
	f.Exec(t, `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'agent',$3,'agent reply','comment')`, issue, f.WorkspaceID, agent)
	wakeDispatch(t, s, w)
	current, err := f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: parseTestUUID(t, f.WorkspaceID)})
	if err != nil || !current.Enabled || current.LastTaskID.Valid {
		t.Fatalf("nonmatching input consumed once rule: %+v %v", current, err)
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); got != 0 {
		t.Fatalf("nonmatching receipt: %d", got)
	}
	f.Exec(t, `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'requested reply','comment')`, issue, f.WorkspaceID, other)
	wakeDispatch(t, s, w)
	if got := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); got != 1 {
		t.Fatalf("matching comment did not enqueue: %d", got)
	}
	current, err = f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: parseTestUUID(t, f.WorkspaceID)})
	if err != nil || current.Enabled {
		t.Fatal("one-shot did not finish")
	}
}

func TestIssueWakeupActorMatchesEditorNotOriginalAuthor(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	other := f.User(t, "editor", "wakeup-editor@multica.test")
	f.Member(t, f.WorkspaceID, other, "member")
	comment := f.Comment(t, util.UUIDToString(issue), "original")
	makeRule := func(kind, id string) db.IssueWakeup {
		return wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.updated"}, FilterActorType: kind, FilterActorID: id, Instruction: "check edit"})
	}
	authorRule, editorRule, agentRule := makeRule("member", f.UserID), makeRule("member", other), makeRule("agent", agent)
	edit := func(kind, id, body string, commit bool) {
		t.Helper()
		ctx := context.Background()
		tx, err := f.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT set_config('multica.actor_type',$1,true),set_config('multica.actor_id',$2,true)", kind, id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE comment SET content=$2 WHERE id=$1", comment, body); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := func(w db.IssueWakeup) int {
		return f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID)
	}
	edit("member", other, "human edit", true)
	if count(authorRule) != 0 || count(editorRule) != 1 || count(agentRule) != 0 {
		t.Fatal("editor confused with original author")
	}
	edit("agent", agent, "agent edit", true)
	if count(authorRule) != 0 || count(editorRule) != 1 || count(agentRule) != 1 {
		t.Fatal("agent mutation did not match actor")
	}
	edit("member", f.UserID, "rolled back", false)
	if count(authorRule) != 0 {
		t.Fatal("rolled back edit captured")
	}
}

func TestIssueWakeupActorValidationAndWorkspaceScope(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	base := WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, FilterActorType: "member", FilterActorID: f.UserID, Instruction: "check"}
	for _, change := range []func(*WakeupInput){
		func(in *WakeupInput) { in.FilterActorType = "system" }, func(in *WakeupInput) { in.FilterActorType = "" }, func(in *WakeupInput) { in.FilterActorID = "" },
		func(in *WakeupInput) { in.FilterAgentID = agent }, func(in *WakeupInput) { in.EventTypes = []string{"task.completed"} },
		func(in *WakeupInput) { in.Kind = "at"; in.EventTypes = nil; in.AfterSeconds = 60 },
	} {
		in := base
		change(&in)
		if _, err := s.Validate(&in, time.Now()); !errors.Is(err, ErrWakeupInput) {
			t.Fatalf("accepted invalid actor filter: %+v %v", in, err)
		}
	}
	foreign, _, _, foreignAgent := wakeFixture(t)
	for _, tc := range []struct {
		kind, id string
		want     error
	}{{"member", foreign.UserID, ErrWakeupForbidden}, {"agent", foreignAgent, ErrWakeupForbidden}, {"member", "bad-id", ErrWakeupInput}} {
		in := base
		in.FilterActorType = tc.kind
		in.FilterActorID = tc.id
		if _, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, in); !errors.Is(err, tc.want) {
			t.Fatalf("scope error %v, want %v", err, tc.want)
		}
	}
}

func TestIssueWakeupActorFilterSurvivesEnableAndExplicitReplacement(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	owner := parseTestUUID(t, f.UserID)
	in := WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, FilterActorType: "member", FilterActorID: f.UserID, Instruction: "check"}
	w := wakeCreate(t, f, s, issue, in)
	disabled, err := s.Disable(ctx, issue, w.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := s.Enable(ctx, issue, owner, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: disabled.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if enabled.FilterActorType.String != "member" || enabled.FilterActorID != owner {
		t.Fatal("enable lost filter")
	}
	f.Comment(t, util.UUIDToString(issue), "matching old rule")
	in.FilterActorType = "agent"
	in.FilterActorID = agent
	replaced, err := s.Save(ctx, issue, owner, pgtype.UUID{}, w.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.FilterActorType.String != "agent" || replaced.Revision != enabled.Revision+1 {
		t.Fatal("replacement lost actor filter")
	}
	f.Comment(t, util.UUIDToString(issue), "no longer matching")
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); got != 0 {
		t.Fatal("old rule input survived revision")
	}
	in.FilterActorType = ""
	in.FilterActorID = ""
	cleared, err := s.Save(ctx, issue, owner, pgtype.UUID{}, w.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.FilterActorType.Valid || cleared.FilterActorID.Valid {
		t.Fatal("explicit replacement could not clear filter")
	}
}

func TestIssueWakeupLegacyMutationAgentFilterNormalizes(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, FilterAgentID: agent, Instruction: "wait"})
	if w.FilterAgentID.Valid || w.FilterActorType.String != "agent" || w.FilterActorID != parseTestUUID(t, agent) {
		t.Fatalf("legacy alias not normalized: %+v", w)
	}
	f.Comment(t, util.UUIDToString(issue), "human")
	if f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID) != 0 {
		t.Fatal("human matched legacy agent alias")
	}
	f.Exec(t, `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'agent',$3,'agent reply','comment')`, issue, f.WorkspaceID, agent)
	wakeDispatch(t, s, w)
	if f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)) != 1 {
		t.Fatal("legacy alias did not wake on agent comment")
	}
	// Existing clients can still combine task and mutation types in one rule.
	in := WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.completed", "comment.created"}, FilterAgentID: agent, Instruction: "wait"}
	if _, err := s.Validate(&in, time.Now()); err != nil || in.FilterAgentID != agent || in.FilterActorType != "" {
		t.Fatalf("mixed legacy subscription changed: %+v %v", in, err)
	}
}
