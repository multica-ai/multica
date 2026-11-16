package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWakeupChildrenReportedSelection(t *testing.T) {
	type child struct {
		status string
		stage  int
	}
	for _, tc := range []struct {
		name                             string
		children                         []child
		stage                            int
		met                              bool
		review, closed, cancelled, total int
	}{
		{"all in review", []child{{"in_review", 0}, {"in_review", 0}}, 0, true, 2, 0, 0, 2},
		{"mixed reports", []child{{"in_review", 0}, {"done", 0}, {"cancelled", 0}}, 0, true, 1, 2, 1, 3},
		{"todo waits", []child{{"in_review", 0}, {"todo", 0}}, 0, false, 1, 0, 0, 2},
		{"in progress waits", []child{{"in_review", 0}, {"in_progress", 0}}, 0, false, 1, 0, 0, 2},
		{"blocked waits", []child{{"in_review", 0}, {"blocked", 0}}, 0, false, 1, 0, 0, 2},
		{"empty waits", nil, 0, false, 0, 0, 0, 0},
		{"stage frontier excludes later and unstaged", []child{{"done", 1}, {"in_review", 2}, {"todo", 3}, {"blocked", 0}}, 2, true, 1, 1, 0, 2},
		{"earlier stage waits", []child{{"todo", 1}, {"in_review", 2}}, 2, false, 1, 0, 0, 2},
		{"missing stage waits", []child{{"in_review", 1}, {"done", 3}}, 2, false, 1, 0, 0, 1},
		{"no stage includes unstaged", []child{{"in_review", 1}, {"todo", 0}}, 0, false, 1, 0, 0, 2},
		{"custom terminal categories", []child{{"shipped", 0}, {"abandoned", 0}, {"in_review", 0}}, 0, true, 1, 2, 1, 3},
		{"custom started is not a review handoff", []child{{"reviewing", 0}}, 0, false, 0, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			for i, status := range []struct{ key, category string }{{"shipped", "done"}, {"abandoned", "closed"}, {"reviewing", "started"}} {
				f.Insert(t, "issue_status", testutil.Cols{"workspace_id": f.WorkspaceID, "key": status.key, "name": status.key, "category": status.category, "color": "#123456", "position": 100 + i})
			}
			for _, ch := range tc.children {
				cols := testutil.Cols{"parent_issue_id": issue, "status": ch.status}
				if ch.stage > 0 {
					cols["stage"] = ch.stage
				}
				f.Issue(t, "worker", cols)
			}
			predicate := map[string]any{"type": "children_reported"}
			if tc.stage > 0 {
				predicate["stage"] = tc.stage
			}
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Inspect worker results", Condition: condition(t, predicate)})
			tx, err := f.Pool.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			met, _, observed, err := s.evaluateCondition(context.Background(), tx, f.q.WithTx(tx), w)
			if err != nil {
				t.Fatal(err)
			}
			if met != tc.met || observed["review_ready"] != tc.review || observed["closed"] != tc.closed || observed["cancelled"] != tc.cancelled || observed["total"] != tc.total {
				t.Fatalf("met=%t observed=%v, want %+v", met, observed, tc)
			}
		})
	}
}

// Exercise the scheduler's polling lane, receipt, and queued task, not just
// the predicate helper. A false observation rearms continuous conditions.
func TestWakeupChildrenReportedScheduler(t *testing.T) {
	for _, mode := range []string{"once", "continuous"} {
		t.Run(mode, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			child := f.Issue(t, "worker", testutil.Cols{"parent_issue_id": issue, "status": "in_progress"})
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: mode, Instruction: "Inspect results without accepting them", Condition: condition(t, map[string]any{"type": "children_reported"})})
			// An unselected terminal-only condition must still wait for review.
			control := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Wait for closure", Condition: condition(t, map[string]any{"type": "children_done"})})
			tick := func(want int) db.IssueWakeup {
				t.Helper()
				f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE issue_id=$1 AND enabled", issue)
				if err := s.TickWorkspaces(context.Background(), parseTestUUID(t, f.WorkspaceID)); err != nil {
					t.Fatal(err)
				}
				got, err := f.q.LocklessWakeup(context.Background(), w.ID)
				if err != nil {
					t.Fatal(err)
				}
				if n := wakeRuns(t, f, w.ID); n != want {
					t.Fatalf("runs=%d want=%d; rule=%+v", n, want, got)
				}
				return got
			}
			tick(0)
			wakeSetStatus(t, f, parseTestUUID(t, child), "in_review")
			got := tick(1)
			if (mode == "continuous") != got.Enabled || got.FireCount != 1 {
				t.Fatalf("unexpected rule after report: %+v", got)
			}
			if n := wakeRuns(t, f, control.ID); n != 0 {
				t.Fatalf("children_done fired on review: %d", n)
			}
			var payload struct {
				Observed  map[string]int `json:"observed"`
				Condition map[string]any `json:"condition"`
			}
			var raw []byte
			f.QueryRow(t, "SELECT payload FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type='condition.met'", w.ID).Scan(&raw)
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Condition["type"] != "children_reported" || payload.Observed["review_ready"] != 1 || payload.Observed["closed"] != 0 || payload.Observed["total"] != 1 {
				t.Fatalf("receipt: %s", raw)
			}
			t.Logf("condition.met receipt: %s", raw)
			task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != "queued" {
				t.Fatalf("report did not queue a run: %+v", task)
			}
			t.Logf("queued task: id=%s status=%s", util.UUIDToString(task.ID), task.Status)
			var status string
			f.QueryRow(t, "SELECT status FROM issue WHERE id=$1", child).Scan(&status)
			if status != "in_review" {
				t.Fatalf("condition accepted review: %s", status)
			}
			tick(1)
			f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", task.ID)
			wakeSetStatus(t, f, parseTestUUID(t, child), "blocked")
			tick(1)
			wakeSetStatus(t, f, parseTestUUID(t, child), "in_review")
			want := 1
			if mode == "continuous" {
				want = 2
			}
			tick(want)
		})
	}
}

func TestWakeupChildrenReportedDynamicMembership(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	child := f.Issue(t, "reported", testutil.Cols{"parent_issue_id": issue, "status": "in_review", "stage": 1})
	f.Issue(t, "grandchild is not selected", testutil.Cols{"parent_issue_id": child, "status": "blocked", "stage": 1})
	waiting := f.Issue(t, "waiting", testutil.Cols{"parent_issue_id": issue, "status": "todo", "stage": 1})
	// Even a malformed cross-workspace parent link must not affect the count.
	foreignWS := f.Workspace(t, "foreign", "reported-foreign")
	f.Issue(t, "foreign child", testutil.Cols{"workspace_id": foreignWS, "parent_issue_id": issue, "status": "blocked"})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Inspect", Condition: condition(t, map[string]any{"type": "children_reported", "stage": 1})})
	check := func(want bool, total int) {
		t.Helper()
		tx, err := f.Pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		met, _, observed, err := s.evaluateCondition(context.Background(), tx, f.q.WithTx(tx), w)
		if err != nil || met != want || observed["total"] != total {
			t.Fatalf("met=%t observed=%v err=%v; want %t total=%d", met, observed, err, want, total)
		}
	}
	check(false, 2)
	f.Exec(t, "UPDATE issue SET stage=2 WHERE id=$1", waiting)
	check(true, 1)
	f.Exec(t, "UPDATE issue SET stage=1 WHERE id=$1", waiting)
	check(false, 2)
	f.Exec(t, "UPDATE issue SET parent_issue_id=NULL WHERE id=$1", waiting)
	check(true, 1)
	added := f.Issue(t, "new worker", testutil.Cols{"parent_issue_id": issue, "status": "blocked", "stage": 1})
	check(false, 2)
	f.Exec(t, "DELETE FROM issue WHERE id=$1", added)
	check(true, 1)
	f.Exec(t, "UPDATE issue SET stage=2 WHERE id=$1", child)
	check(false, 0)
	f.Exec(t, "UPDATE issue SET stage=1 WHERE id=$1", child)
	check(true, 1)
}

func TestWakeupChildrenReportedRejectsInvalidInputs(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	for name, in := range map[string]WakeupInput{
		"stage zero":       {Condition: json.RawMessage(`{"type":"children_reported","stage":0}`)},
		"stage negative":   {Condition: json.RawMessage(`{"type":"children_reported","stage":-1}`)},
		"stage too large":  {Condition: json.RawMessage(`{"type":"children_reported","stage":1001}`)},
		"stage fractional": {Condition: json.RawMessage(`{"type":"children_reported","stage":1.5}`)},
		"system only":      {Condition: json.RawMessage(`{"type":"children_reported","each_stage":true}`)},
		"events":           {EventTypes: []string{"comment.created"}},
		"agent filter":     {FilterAgentID: agent},
		"actor filter":     {FilterActorType: "member", FilterActorID: f.UserID},
		"task filter":      {FilterTaskID: util.UUIDToString(issue)},
		"time kind":        {Kind: "at", AfterSeconds: 60},
		"event schedule":   {AfterSeconds: 60},
	} {
		t.Run(name, func(t *testing.T) {
			if len(in.Condition) == 0 {
				in.Condition = json.RawMessage(`{"type":"children_reported"}`)
			}
			if in.Kind == "" {
				in.Kind = "event"
			}
			in.AgentID, in.Instruction = agent, "Inspect"
			if _, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, in); !errors.Is(err, ErrWakeupInput) {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
	outsider := f.User(t, "outsider", "reported-outsider@multica.test")
	f.Member(t, f.WorkspaceID, outsider, "member")
	_, err := s.Create(context.Background(), issue, parseTestUUID(t, outsider), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Inspect", Condition: json.RawMessage(`{"type":"children_reported"}`)})
	if !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("borrowed invoke rights: %v", err)
	}
}
