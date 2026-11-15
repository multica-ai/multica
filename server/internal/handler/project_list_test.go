package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseProjectListQueryRejectsUnknownValues(t *testing.T) {
	for _, raw := range []string{
		"status=done",
		"priority=p0",
		"lead=someone",
		"lead=squad:" + testUserID,
		"lead=member:not-a-uuid",
		"sort=updated",
		"direction=up",
		"limit=0",
		"limit=201",
		"offset=-1",
		"archived=yes",
	} {
		q, _ := url.ParseQuery(raw)
		if _, err := parseProjectListQuery(q); err == nil {
			t.Errorf("%s: expected an error", raw)
		}
	}
}

func TestParseProjectListQueryReadsRepeatedAndCommaValues(t *testing.T) {
	q, _ := url.ParseQuery("status=planned,paused&status=in_progress&lead=none&sort=due_date")
	got, err := parseProjectListQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.statuses, []string{"planned", "paused", "in_progress"}) {
		t.Fatalf("statuses = %v", got.statuses)
	}
	if got.sort != "due_date" || got.desc {
		t.Fatalf("due_date must default to ascending, got sort=%s desc=%v", got.sort, got.desc)
	}

	q, _ = url.ParseQuery("")
	if got, _ = parseProjectListQuery(q); got.sort != "created_at" || !got.desc || got.limit != 0 {
		t.Fatalf("default must be every project, newest first; got %+v", got)
	}
}

func TestProjectListArchivedOnlyKeepsArchivedProjects(t *testing.T) {
	q, _ := url.ParseQuery("archived=only")
	got, err := parseProjectListQuery(q)
	if err != nil || !got.includeArchived || !got.archivedOnly {
		t.Fatalf("archived=only = %+v, %v", got, err)
	}
	archived := db.Project{Status: "completed", ArchivedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}
	if !got.matchesProject(archived) || got.matchesProject(db.Project{Status: "completed"}) {
		t.Fatal("archived=only must keep archived projects and drop the rest")
	}

	q, _ = url.ParseQuery("include_archived=true")
	if got, _ = parseProjectListQuery(q); !got.includeArchived || got.archivedOnly {
		t.Fatalf("include_archived=true = %+v", got)
	}
}

func TestSortProjectResponsesKeepsUndatedLast(t *testing.T) {
	day := func(s string) *string { return &s }
	projects := []ProjectResponse{
		{ID: "a", Title: "A", DueDate: day("2026-02-01")},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C", DueDate: day("2026-01-01")},
	}
	ids := func() []string {
		out := make([]string, len(projects))
		for i, p := range projects {
			out[i] = p.ID
		}
		return out
	}
	sortProjectResponses(projects, "due_date", false)
	if got := ids(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("asc = %v", got)
	}
	sortProjectResponses(projects, "due_date", true)
	if got := ids(); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("desc = %v", got)
	}
}

func listProjectsForTest(t *testing.T, query string) (titles []string, total int, hasMore bool) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list %s: expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var body struct {
		Projects []ProjectResponse `json:"projects"`
		Total    int               `json:"total"`
		HasMore  bool              `json:"has_more"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, p := range body.Projects {
		titles = append(titles, p.Title)
	}
	return titles, body.Total, body.HasMore
}

func TestListProjectsFiltersSortsAndPages(t *testing.T) {
	create := func(body map[string]any) ProjectResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, body))
		p := decodeProject(t, w, http.StatusCreated)
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, p.ID)
		})
		return p
	}
	create(map[string]any{"title": "plist alpha", "status": "planned", "priority": "high", "due_date": "2026-03-01",
		"lead_type": "member", "lead_id": testUserID})
	create(map[string]any{"title": "plist beta", "status": "in_progress", "priority": "low", "due_date": "2026-01-15"})
	create(map[string]any{"title": "plist gamma", "status": "completed", "priority": "high"})
	create(map[string]any{"title": "other project", "status": "planned", "priority": "high"})

	titles, total, _ := listProjectsForTest(t, "q=PLIST&sort=due_date")
	if !slices.Equal(titles, []string{"plist beta", "plist alpha", "plist gamma"}) || total != 3 {
		t.Fatalf("search + due_date sort = %v (total %d)", titles, total)
	}

	titles, _, _ = listProjectsForTest(t, "q=plist&status=planned,completed&priority=high&sort=name")
	if !slices.Equal(titles, []string{"plist alpha", "plist gamma"}) {
		t.Fatalf("status + priority filter = %v", titles)
	}

	titles, _, _ = listProjectsForTest(t, "q=plist&lead=member:"+testUserID)
	if !slices.Equal(titles, []string{"plist alpha"}) {
		t.Fatalf("lead filter = %v", titles)
	}
	titles, _, _ = listProjectsForTest(t, "q=plist&lead=none&sort=name")
	if !slices.Equal(titles, []string{"plist beta", "plist gamma"}) {
		t.Fatalf("no-lead filter = %v", titles)
	}

	titles, total, hasMore := listProjectsForTest(t, "q=plist&sort=name&limit=2")
	if !slices.Equal(titles, []string{"plist alpha", "plist beta"}) || total != 3 || !hasMore {
		t.Fatalf("first page = %v total=%d has_more=%v", titles, total, hasMore)
	}
	titles, _, hasMore = listProjectsForTest(t, "q=plist&sort=name&limit=2&offset=2")
	if !slices.Equal(titles, []string{"plist gamma"}) || hasMore {
		t.Fatalf("last page = %v has_more=%v", titles, hasMore)
	}

	w := httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&status=done", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown status: expected 400, got %d", w.Code)
	}
}

func TestProjectLastActivityFollowsItsIssues(t *testing.T) {
	ctx := context.Background()
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "plast activity",
	}))
	project := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, project.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, project.ID)
	})
	if project.LastActivityAt != project.UpdatedAt {
		t.Fatalf("a project without issues is as active as its last edit: %q vs %q", project.LastActivityAt, project.UpdatedAt)
	}

	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "plast issue", "project_id": project.ID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", w.Code, w.Body.String())
	}
	projectEdit := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issueActivity := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := testPool.Exec(ctx, `UPDATE project SET updated_at = $2 WHERE id = $1`, project.ID, projectEdit); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE issue SET updated_at = $2, last_activity_at = $3 WHERE project_id = $1`,
		project.ID, projectEdit, issueActivity); err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	testHandler.ListProjects(w, newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&q=plast", nil))
	var body struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil || len(body.Projects) != 1 {
		t.Fatalf("list: %v %s", err, w.Body.String())
	}
	got, err := time.Parse(time.RFC3339Nano, body.Projects[0].LastActivityAt)
	if err != nil || !got.Equal(issueActivity) {
		t.Fatalf("last_activity_at = %q, want the issue's %s", body.Projects[0].LastActivityAt, issueActivity)
	}

	w = httptest.NewRecorder()
	testHandler.GetProject(w, withURLParam(newRequest("GET", "/api/projects/"+project.ID, nil), "id", project.ID))
	detail := decodeProject(t, w, http.StatusOK)
	if detail.LastActivityAt != body.Projects[0].LastActivityAt {
		t.Fatalf("detail last_activity_at = %q, list %q", detail.LastActivityAt, body.Projects[0].LastActivityAt)
	}
}
