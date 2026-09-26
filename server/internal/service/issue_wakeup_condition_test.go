package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// conditionFixture is wakeFixture plus cleanup for the timeline entries rules
// write.
func conditionFixture(t *testing.T) (principalFixture, *IssueWakeupService, pgtype.UUID, string) {
	t.Helper()
	f, s, issue, agent := wakeFixture(t)
	f.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	return f, s, issue, agent
}

// wakeTick makes the rule due and dispatches it, as the scheduler would.
func wakeTick(t *testing.T, f principalFixture, s *IssueWakeupService, id pgtype.UUID) db.IssueWakeup {
	t.Helper()
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1 AND next_fire_at IS NOT NULL", id)
	w, err := f.q.LocklessWakeup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wakeDispatch(t, s, w)
	w, err = f.q.LocklessWakeup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func wakeRuns(t *testing.T, f principalFixture, id pgtype.UUID) int {
	t.Helper()
	return f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(id))
}

func condition(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWakeupConditionFieldValueWakesOnceWhenItHolds(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Review when ready",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})})
	if len(w.EventTypes) != 1 || w.EventTypes[0] != "issue.status_changed" || w.Mode != "once" {
		t.Fatalf("condition rule = %+v", w)
	}
	// Not yet true: no run, no matter how often it is evaluated.
	wakeTick(t, f, s, w.ID)
	wakeSetStatus(t, f, issue, "in_progress")
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("unmet condition started %d runs", n)
	}
	// The matching status change is a hint; the platform evaluates it.
	wakeSetStatus(t, f, issue, "in_review")
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("met condition started %d runs, want 1", n)
	}
	if got.Enabled {
		t.Fatal("a once condition must be consumed")
	}
	task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, "condition.met") || !strings.Contains(task.HandoffNote.String, `"status":"in_review"`) {
		t.Fatalf("trigger facts missing the observation: %s", task.HandoffNote.String)
	}
	// Status-change hints never reach the agent as separate facts.
	if strings.Contains(task.HandoffNote.String, "issue.status_changed") {
		t.Fatalf("hint leaked into the run: %s", task.HandoffNote.String)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_triggered'", issue); n != 1 {
		t.Fatalf("trigger timeline entries = %d, want 1", n)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_created'", issue); n != 1 {
		t.Fatalf("created timeline entries = %d, want 1", n)
	}
}

func TestWakeupConditionRepeatsOnlyWhenItBecomesTrueAgain(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	label := f.Insert(t, "issue_label", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "approved", "color": "#123456"})
	f.Cleanup(t, "DELETE FROM issue_to_label WHERE label_id=$1", label)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Ship it",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "label", "label_id": label})})
	if !w.MaxFires.Valid || w.MaxFires.Int32 != wakeupDefaultMaxFires {
		t.Fatalf("repeating rule cap = %+v, want default", w.MaxFires)
	}
	f.Exec(t, "INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)", issue, label)
	wakeTick(t, f, s, w.ID)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("a condition that stays true fired %d times, want 1", n)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID))
	f.Exec(t, "DELETE FROM issue_to_label WHERE issue_id=$1 AND label_id=$2", issue, label)
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)", issue, label)
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 2 {
		t.Fatalf("re-satisfied condition fired %d times total, want 2", n)
	}
	if !got.Enabled || got.FireCount != 2 {
		t.Fatalf("continuous rule after two fires: enabled=%t fire_count=%d", got.Enabled, got.FireCount)
	}
}

func TestWakeupConditionChildrenAndOtherIssue(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	first := f.Issue(t, "Stage 1 child", testutil.Cols{"parent_issue_id": issue, "status": "todo", "stage": 1})
	f.Issue(t, "Stage 2 child", testutil.Cols{"parent_issue_id": issue, "status": "todo", "stage": 2})
	other := f.Issue(t, "Blocking issue", testutil.Cols{"status": "in_progress"})
	stage := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Start stage 2",
		Condition: condition(t, map[string]any{"type": "children_done", "stage": 1})})
	watch := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Continue after the blocker",
		Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": other, "state": "done"})})
	wakeTick(t, f, s, stage.ID)
	wakeTick(t, f, s, watch.ID)
	if wakeRuns(t, f, stage.ID)+wakeRuns(t, f, watch.ID) != 0 {
		t.Fatal("unmet conditions started runs")
	}
	var watched WakeupCondition
	_ = json.Unmarshal(watch.Condition, &watched)
	if !strings.Contains(watched.Identifier, "-") {
		t.Fatalf("watched issue identifier not recorded: %s", watch.Condition)
	}
	f.Exec(t, "UPDATE issue SET status='cancelled' WHERE id=$1", first)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", other)
	wakeTick(t, f, s, stage.ID)
	wakeTick(t, f, s, watch.ID)
	// Both hold for the same agent: the second joins the first one's run
	// instead of queuing another behind it.
	if wakeRuns(t, f, stage.ID) != 1 || wakeRuns(t, f, watch.ID) != 0 {
		t.Fatalf("stage runs=%d, other-issue runs=%d, want one shared run", wakeRuns(t, f, stage.ID), wakeRuns(t, f, watch.ID))
	}
	var shared []byte
	f.QueryRow(t, "SELECT context FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(stage.ID)).Scan(&shared)
	if notes := JoinedWakeupNotes(shared); !strings.Contains(notes, "Continue after the blocker") || !strings.Contains(notes, `"state":"done"`) {
		t.Fatalf("shared run lacks the joined rule: %q", notes)
	}
}

func TestWakeupConditionPullRequestChecksWaitForNewResult(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	var pr string
	if err := f.Pool.QueryRow(ctx, `INSERT INTO github_pull_request(workspace_id,installation_id,repo_owner,repo_name,pr_number,title,state,html_url,pr_created_at,pr_updated_at,snapshot_head_sha,checks_rollup_state)
		VALUES($1,1,'multica-ai','wakeup-test',(extract(epoch from clock_timestamp())*1000)::bigint % 100000,'PR','open','https://example.test/pr',now(),now(),'aaa','SUCCESS') RETURNING id`, f.WorkspaceID).Scan(&pr); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM github_pull_request WHERE id=$1", pr)
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
	// Finished checks from before registration belong to an older push.
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "UPDATE github_pull_request SET snapshot_head_sha='bbb',checks_rollup_state='PENDING' WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("stale or pending checks started %d runs", n)
	}
	f.Exec(t, "UPDATE github_pull_request SET checks_rollup_state='FAILURE' WHERE id=$1", pr)
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("finished checks started %d runs, want 1", n)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, `"checks":"failure"`) || !strings.Contains(task.HandoffNote.String, `"head_sha":"bbb"`) {
		t.Fatalf("PR facts missing: %s", task.HandoffNote.String)
	}
}

func TestWakeupConditionValidation(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	member := parseTestUUID(t, f.UserID)
	for name, in := range map[string]WakeupInput{
		"unknown status":    {Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "nope"})},
		"this issue":        {Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": util.UUIDToString(issue), "state": "done"})},
		"unknown field":     {Condition: condition(t, map[string]any{"type": "issue_field", "field": "title", "value": "x"})},
		"extra key":         {Condition: condition(t, map[string]any{"type": "children_done", "when": "soon"})},
		"with events":       {EventTypes: []string{"comment.created"}, Condition: condition(t, map[string]any{"type": "children_done"})},
		"time kind":         {Kind: "at", AfterSeconds: 60, Condition: condition(t, map[string]any{"type": "children_done"})},
		"cap on once":       {MaxFires: 3, Condition: condition(t, map[string]any{"type": "children_done"})},
		"foreign assignee":  {Condition: condition(t, map[string]any{"type": "issue_field", "field": "assignee", "assignee_type": "agent", "assignee_id": util.UUIDToString(parseTestUUID(t, "00000000-0000-0000-0000-000000000001"))})},
		"property archived": {Condition: condition(t, map[string]any{"type": "issue_field", "field": "property", "property_id": "00000000-0000-0000-0000-000000000002", "value": "x"})},
	} {
		in.AgentID, in.Instruction = agent, "x"
		if in.Kind == "" {
			in.Kind = "event"
		}
		if _, err := s.Create(ctx, issue, member, pgtype.UUID{}, in); !errors.Is(err, ErrWakeupInput) {
			t.Errorf("%s: err = %v, want ErrWakeupInput", name, err)
		}
	}
}
