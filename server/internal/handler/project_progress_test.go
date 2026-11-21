package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

func builtInCategory(status string) string {
	category, _ := issuestatus.CategoryForBehavior(status)
	return category
}

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildProjectProgressHistoryReplaysStatusChanges(t *testing.T) {
	now := day("2026-11-09").Add(15 * time.Hour)
	issues := []progressIssue{
		// Done on Oct 20, after starting on Oct 14.
		{status: "done", createdAt: day("2026-10-12"), changes: []progressStatusChange{
			{at: day("2026-10-14").Add(time.Hour), from: "todo"},
			{at: day("2026-10-20").Add(time.Hour), from: "in_progress"},
		}},
		// Created on Oct 30, still open.
		{status: "todo", createdAt: day("2026-10-30")},
		// Cancelled on Nov 3: in scope before, out of scope after.
		{status: "cancelled", createdAt: day("2026-10-12"), changes: []progressStatusChange{
			{at: day("2026-11-03").Add(time.Hour), from: "backlog"},
		}},
	}

	got := buildProjectProgressHistory(issues, builtInCategory, day("2026-10-12"), now)
	want := []ProjectProgressPoint{
		{Date: "2026-10-12", Scope: 2, Started: 0, Completed: 0},
		{Date: "2026-10-19", Scope: 2, Started: 1, Completed: 0},
		{Date: "2026-10-26", Scope: 2, Started: 0, Completed: 1},
		{Date: "2026-11-02", Scope: 3, Started: 0, Completed: 1},
		{Date: "2026-11-09", Scope: 2, Started: 0, Completed: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("point %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestProjectProgressWindowStartCapsAtAYear(t *testing.T) {
	now := day("2026-11-09")
	if got := projectProgressWindowStart(day("2020-01-01"), now); !got.Equal(now.AddDate(0, 0, -7*projectProgressMaxWeeks)) {
		t.Fatalf("old project window = %s", got)
	}
	if got := projectProgressWindowStart(day("2027-01-01"), now); !got.Equal(now) {
		t.Fatalf("future start window = %s, want today", got)
	}
	if got := projectProgressWindowStart(day("2026-10-12").Add(9*time.Hour), now); !got.Equal(day("2026-10-12")) {
		t.Fatalf("window = %s, want start day", got)
	}
}

func TestForecastProjectCompletion(t *testing.T) {
	now := day("2026-11-09").Add(10 * time.Hour)
	history := []ProjectProgressPoint{
		{Date: "2026-10-12", Scope: 6, Completed: 0},
		{Date: "2026-10-19", Scope: 8, Completed: 1},
		{Date: "2026-10-26", Scope: 10, Completed: 3},
		{Date: "2026-11-02", Scope: 12, Completed: 4},
		{Date: "2026-11-09", Scope: 11, Completed: 5},
	}
	// 5 completed over the 4 weeks since Oct 12: 1.25 a week, 6 left, 34 days.
	got := forecastProjectCompletion(history, now)
	if got == nil || got.CompletionDate != "2026-12-13" || got.WeeklyVelocity != 1.25 {
		t.Fatalf("forecast = %+v, want 2026-12-13 at 1.25/week", got)
	}

	// A project a week old measures from its first point: 1 a week, 6 left.
	if f := forecastProjectCompletion(history[3:], now); f == nil || f.CompletionDate != "2026-12-21" {
		t.Fatalf("one-week forecast = %+v, want 2026-12-21", f)
	}
	young := []ProjectProgressPoint{{Date: "2026-11-05", Scope: 3}, {Date: "2026-11-09", Scope: 3, Completed: 1}}
	if f := forecastProjectCompletion(young, now); f != nil {
		t.Fatalf("under a week of history forecast = %+v, want nil", f)
	}
	if f := forecastProjectCompletion([]ProjectProgressPoint{{Date: "2026-11-09", Scope: 3}}, now); f != nil {
		t.Fatalf("single point forecast = %+v, want nil", f)
	}
	flat := []ProjectProgressPoint{{Date: "2026-10-12", Scope: 3, Completed: 1}, {Date: "2026-11-09", Scope: 3, Completed: 1}}
	if f := forecastProjectCompletion(flat, now); f != nil {
		t.Fatalf("no velocity forecast = %+v, want nil", f)
	}
	done := []ProjectProgressPoint{{Date: "2026-10-12", Scope: 3}, {Date: "2026-11-09", Scope: 3, Completed: 3}}
	if f := forecastProjectCompletion(done, now); f != nil {
		t.Fatalf("finished project forecast = %+v, want nil", f)
	}
}

func TestSummarizeProjectProgressGroupsByAssignee(t *testing.T) {
	issues := []progressIssue{
		{status: "done", assigneeType: "agent", assigneeID: "a1"},
		{status: "in_progress", assigneeType: "agent", assigneeID: "a1"},
		{status: "done", assigneeType: "member", assigneeID: "m1"},
		{status: "todo"},
		{status: "cancelled", assigneeType: "member", assigneeID: "m1"},
	}
	progress, rows := summarizeProjectProgress(issues, builtInCategory)
	if want := (ProjectProgress{Unit: "issues", Scope: 4, Completed: 2, Started: 1, Unstarted: 1, Cancelled: 1, Percent: 50}); progress != want {
		t.Fatalf("progress = %+v, want %+v", progress, want)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].AssigneeID == nil || *rows[0].AssigneeID != "a1" || rows[0].Scope != 2 || rows[0].Completed != 1 || rows[0].Started != 1 {
		t.Errorf("first row = %+v, want agent a1 1/2 with 1 started", rows[0])
	}
	if rows[1].AssigneeID == nil || *rows[1].AssigneeID != "m1" || rows[1].Scope != 1 {
		t.Errorf("second row = %+v, want member m1 without the cancelled issue", rows[1])
	}
	if rows[2].AssigneeID != nil || rows[2].AssigneeType != nil || rows[2].Scope != 1 {
		t.Errorf("last row = %+v, want unassigned", rows[2])
	}
}

func TestGetProjectProgressEndpoint(t *testing.T) {
	ctx := context.Background()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": "Progress endpoint"})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, project.ID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, project.ID)
	})

	insert := func(status string) string {
		t.Helper()
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, project_id, number, position, assignee_type, assignee_id)
			VALUES ($1, 'member', $2, 'progress '||$3, $3, 'none', $4, $5, 0, 'member', $2)
			RETURNING id
		`, testWorkspaceID, testUserID, status, project.ID, number).Scan(&id); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		return id
	}
	doneID := insert("done")
	insert("cancelled")
	insert("in_progress")
	insert("todo")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details)
		VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"in_progress","to":"done"}'::jsonb)
	`, testWorkspaceID, doneID, testUserID); err != nil {
		t.Fatalf("insert activity: %v", err)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequest("GET", "/api/projects/"+project.ID+"/progress", nil), "id", project.ID)
	testHandler.GetProjectProgress(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProjectProgress: %d %s", w.Code, w.Body.String())
	}
	var resp ProjectProgressResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if want := (ProjectProgress{Unit: "issues", Scope: 3, Completed: 1, Started: 1, Unstarted: 1, Cancelled: 1, Percent: 33}); resp.Progress != want {
		t.Errorf("progress = %+v, want %+v", resp.Progress, want)
	}
	if len(resp.ByAssignee) != 1 || resp.ByAssignee[0].Scope != 3 || resp.ByAssignee[0].Completed != 1 {
		t.Errorf("by_assignee = %+v, want one member row 1/3", resp.ByAssignee)
	}
	if len(resp.History) != 1 {
		t.Fatalf("history = %+v, want today only for a new project", resp.History)
	}
	if last := resp.History[0]; last.Scope != 3 || last.Completed != 1 || last.Started != 1 {
		t.Errorf("today's point = %+v, want it to match current progress", last)
	}
	if resp.Forecast != nil {
		t.Errorf("forecast = %+v, want nil without a week of history", resp.Forecast)
	}

	// The project response carries the same summary, and keeps done_count's
	// terminal meaning for older clients.
	w = httptest.NewRecorder()
	req = withURLParam(newRequest("GET", "/api/projects/"+project.ID, nil), "id", project.ID)
	testHandler.GetProject(w, req)
	var got ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if got.IssueCount != 4 || got.DoneCount != 2 || got.Progress != resp.Progress {
		t.Errorf("GetProject = issue_count %d, done_count %d, progress %+v", got.IssueCount, got.DoneCount, got.Progress)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequest("GET", "/api/projects/00000000-0000-0000-0000-000000000000/progress", nil), "id", "00000000-0000-0000-0000-000000000000")
	testHandler.GetProjectProgress(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("missing project: %d, want 404", w.Code)
	}
}
