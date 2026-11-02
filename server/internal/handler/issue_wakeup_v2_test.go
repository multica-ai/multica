package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A status write outside the HTTP handlers still records the child's change
// in its own transaction; the sweep turns it into the parent's wake exactly
// once.
func TestChildDoneRecordedByAnyWriterAndSweptOnce(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sweep := func() {
		t.Helper()
		if err := (&service.IssueWakeupService{Tasks: testHandler.TaskService}).SweepChildEvents(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", fx.child.ID)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_child_event WHERE parent_id=$1 AND child_id=$2 AND kind='closed' AND processed_at IS NULL", fx.parent.ID, fx.child.ID); n != 1 {
		t.Fatalf("recorded closings = %d, want 1", n)
	}
	// Moving between closed statuses is not a change the rule cares about.
	dbfx.Exec(t, "UPDATE issue SET status='cancelled' WHERE id=$1", fx.child.ID)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_child_event WHERE parent_id=$1 AND kind IN ('closed','reopened')", fx.parent.ID); n != 1 {
		t.Fatalf("closed-to-closed recorded %d changes, want 1", n)
	}
	// Fresh rows belong to the request that wrote them; the sweep waits.
	sweep()
	if got := len(childDoneEntries(t, fx.parent.ID)); got != 0 {
		t.Fatalf("sweep took a fresh change: %d entries", got)
	}
	dbfx.Exec(t, "UPDATE issue_child_event SET created_at=created_at-interval '1 minute' WHERE parent_id=$1", fx.parent.ID)
	sweep()
	sweep()
	if got := len(childDoneEntries(t, fx.parent.ID)); got != 1 {
		t.Fatalf("swept change fired %d entries, want 1", got)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_child_event WHERE parent_id=$1 AND processed_at IS NULL", fx.parent.ID); n != 0 {
		t.Fatalf("%d changes left pending", n)
	}
}

// The request path processes its own change; a later sweep finds nothing.
func TestChildDoneRequestPathLeavesNothingToSweep(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	updateChildStatus(t, fx.child.ID, "done")
	dbfx.Exec(t, "UPDATE issue_child_event SET created_at=created_at-interval '1 hour',claimed_at=claimed_at-interval '1 hour' WHERE parent_id=$1", fx.parent.ID)
	if err := (&service.IssueWakeupService{Tasks: testHandler.TaskService}).SweepChildEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(childDoneEntries(t, fx.parent.ID)); got != 1 {
		t.Fatalf("request plus sweep fired %d entries, want 1", got)
	}
}

// A rule created while the workspace default is off stays off until the
// issue sets its own.
func TestChildDoneWorkspaceDefault(t *testing.T) {
	var settings []byte
	dbfx.QueryRow(t, "SELECT settings FROM workspace WHERE id=$1", testWorkspaceID).Scan(&settings)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "UPDATE workspace SET settings=$2 WHERE id=$1", testWorkspaceID, settings)
	})
	dbfx.Exec(t, `UPDATE workspace SET settings=settings||'{"system_wakeup_child_done":false}' WHERE id=$1`, testWorkspaceID)

	off := newChildDoneFixture(t, "in_progress")
	if rules := listSystemWakeupsFor(t, off.parent.ID); len(rules) != 1 || rules[0].Enabled || rules[0].WorkspaceDefault {
		t.Fatalf("workspace default not applied: %+v", rules)
	}
	updateChildStatus(t, off.child.ID, "done")
	if got := len(childDoneEntries(t, off.parent.ID)); got != 0 {
		t.Fatalf("rule off by default still fired: %d", got)
	}
	// The issue's own setting wins over the default; a partial body keeps
	// the instruction.
	on := newChildDoneFixture(t, "in_progress")
	if w := putChildDoneRule(t, on.parent.ID, map[string]any{"instruction": "Check the window first."}); w.Code != http.StatusOK {
		t.Fatalf("save instruction: %d %s", w.Code, w.Body.String())
	}
	if w := putChildDoneRule(t, on.parent.ID, map[string]any{"enabled": true}); w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	if rules := listSystemWakeupsFor(t, on.parent.ID); len(rules) != 1 || !rules[0].Enabled || rules[0].Instruction != "Check the window first." {
		t.Fatalf("override: %+v", rules)
	}
	updateChildStatus(t, on.child.ID, "done")
	if got := len(childDoneEntries(t, on.parent.ID)); got != 1 {
		t.Fatalf("issue override on: %d entries, want 1", got)
	}
}

type workspaceWakeupRow struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	Source          string  `json:"source"`
	Rule            *string `json:"rule"`
	SystemStage     *int    `json:"system_stage"`
	SystemRemaining *int    `json:"system_remaining"`
	Revision        *int64  `json:"revision"`
	Enabled         bool    `json:"enabled"`
	PausedReason    *string `json:"paused_reason"`
	Runs7d          int     `json:"runs_7d"`
	CanManage       bool    `json:"can_manage"`
	Condition       any     `json:"condition"`
}

func listWorkspaceWakeupRows(t *testing.T, query string) ([]workspaceWakeupRow, map[string]int) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceWakeups(rec, newRequest("GET", "/api/issue-wakeups?"+query, nil))
	if rec.Code != 200 {
		t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		Items  []workspaceWakeupRow `json:"items"`
		Counts map[string]int       `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Items, out.Counts
}

func TestWorkspaceWakeupsListSourcesSystemRulesAndPaused(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	createStagedChild(t, fx.parent.ID, 1, "done")
	createStagedChild(t, fx.parent.ID, 1, "todo")
	createStagedChild(t, fx.parent.ID, 2, "backlog")
	agent := dbfx.Agent(t, "list target", testRuntimeID)
	issue := dbfx.Issue(t, "list rules")
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	create := func(in service.WakeupInput) string {
		t.Helper()
		in.AgentID, in.Instruction = agent, "check"
		w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, in)
		if err != nil {
			t.Fatal(err)
		}
		return uuidToString(w.ID)
	}
	member := create(service.WakeupInput{Kind: "event", Condition: json.RawMessage(`{"type":"issue_field","field":"status","value":"in_review"}`)})
	paused := create(service.WakeupInput{Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}})
	dbfx.Exec(t, "UPDATE issue_wakeup SET enabled=false,paused_reason='rate',disabled_at=now() WHERE id=$1", paused)
	for range 2 {
		dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed", "context": fmt.Sprintf(`{"wakeup_id":%q}`, paused)})
	}

	if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"instruction": "Advance the next stage."}); w.Code != http.StatusOK {
		t.Fatalf("create system rule: %d", w.Code)
	}
	rows, counts := listWorkspaceWakeupRows(t, "scope=all&limit=100")
	byID := map[string]workspaceWakeupRow{}
	var system workspaceWakeupRow
	ok := false
	for _, r := range rows {
		byID[r.ID] = r
		if r.IssueID == fx.parent.ID && r.Source == "system" {
			system, ok = r, true
		}
	}
	if !ok || system.Rule == nil || *system.Rule != "child_done" || system.SystemStage == nil || *system.SystemStage != 1 ||
		system.SystemRemaining == nil || *system.SystemRemaining != 1 || !system.Enabled || !system.CanManage || system.Revision == nil {
		t.Fatalf("system row: %+v (found %t)", system, ok)
	}
	if byID[member].Source != "member" || byID[member].Condition == nil {
		t.Fatalf("member row: %+v", byID[member])
	}
	if byID[paused].PausedReason == nil || *byID[paused].PausedReason != "rate" || byID[paused].Runs7d != 2 {
		t.Fatalf("paused row: %+v", byID[paused])
	}
	if counts["paused"] < 1 {
		t.Fatalf("paused count missing: %+v", counts)
	}
	rows, _ = listWorkspaceWakeupRows(t, "scope=paused&limit=100")
	for _, r := range rows {
		if r.PausedReason == nil {
			t.Fatalf("paused scope returned %+v", r)
		}
	}
	rows, _ = listWorkspaceWakeupRows(t, "scope=all&source=system&limit=100")
	for _, r := range rows {
		if r.Source != "system" {
			t.Fatalf("system filter returned %+v", r)
		}
	}
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceWakeups(rec, newRequest("GET", "/api/issue-wakeups?source=robots", nil))
	if rec.Code != 400 {
		t.Fatalf("bad source accepted: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	testHandler.ListPausedWakeups(rec, newRequest("GET", "/api/issue-wakeup-paused", nil))
	var pausedRows []struct {
		IssueID string `json:"issue_id"`
		ID      string `json:"id"`
		Reason  string `json:"paused_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pausedRows); err != nil || rec.Code != 200 {
		t.Fatalf("paused list %d: %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, r := range pausedRows {
		found = found || (r.ID == paused && r.IssueID == issue && r.Reason == "rate")
	}
	if !found {
		t.Fatalf("paused rule missing: %+v", pausedRows)
	}
}

func TestIssueWakeupManagementEndpoints(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup management")
	agent := dbfx.Agent(t, "management target", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{AgentID: agent, Kind: "every", IntervalSeconds: 3600, Instruction: "check staging"})
	if err != nil {
		t.Fatal(err)
	}
	id := uuidToString(w.ID)
	call := func(handler http.HandlerFunc, method string, body any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := withURLParams(newRequest(method, "/", body), "id", issue, "wakeupID", id)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	if rec := call(testHandler.TriggerIssueWakeup, "POST", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("wake now: %d %s", rec.Code, rec.Body.String())
	}
	var run string
	dbfx.QueryRow(t, "SELECT id::text FROM agent_task_queue WHERE context->>'wakeup_id'=$1", id).Scan(&run)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", run)
	// A member cannot check in for the run.
	if rec := call(testHandler.CheckInIssueWakeup, "POST", map[string]any{"note": "fine"}, map[string]string{"X-Task-ID": run}); rec.Code != http.StatusForbidden {
		t.Fatalf("member check-in: %d %s", rec.Code, rec.Body.String())
	}
	agentHeaders := map[string]string{"X-Task-ID": run, "X-Agent-ID": agent, "X-Actor-Source": "task_token"}
	if rec := call(testHandler.CheckInIssueWakeup, "POST", map[string]any{"note": "Nothing changed"}, agentHeaders); rec.Code != http.StatusNoContent {
		t.Fatalf("agent check-in: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(testHandler.ListIssueWakeupRuns, "GET", nil, nil)
	var runs []wakeupRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil || rec.Code != 200 {
		t.Fatalf("runs %d: %s", rec.Code, rec.Body.String())
	}
	if len(runs) != 1 || runs[0].ID != run || runs[0].CheckinNote != "Nothing changed" || len(runs[0].Triggers) != 1 || runs[0].Triggers[0] != "wakeup.manual" {
		t.Fatalf("runs: %+v", runs)
	}
	if rec := call(testHandler.DeleteIssueWakeup, "DELETE", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(testHandler.ListIssueWakeupRuns, "GET", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("runs of a deleted rule: %d", rec.Code)
	}
}
