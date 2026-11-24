package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestListProjectActivityEndpoint(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": "Activity endpoint"}))
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

	var issueID string
	var number int
	if err := testPool.QueryRow(ctx, `
		WITH n AS (
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, project_id, number, position)
		SELECT $1, 'member', $2, 'activity issue', 'in_progress', 'none', $3, issue_counter, 0 FROM n
		RETURNING id, number
	`, testWorkspaceID, testUserID, project.ID).Scan(&issueID, &number); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	// Oldest first; the description autosave must not reach the feed.
	for i, action := range []string{"created", "description_updated", "status_changed", "priority_changed"} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, $4, '{"from":"todo","to":"in_progress"}'::jsonb, now() - make_interval(mins => $5))
		`, testWorkspaceID, issueID, testUserID, action, 10-i); err != nil {
			t.Fatalf("insert %s activity: %v", action, err)
		}
	}

	read := func(t *testing.T, query string) (int, ProjectActivityResponse) {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("GET", "/api/projects/"+project.ID+"/activity"+query, nil), "id", project.ID)
		testHandler.ListProjectActivity(w, req)
		var resp ProjectActivityResponse
		if w.Code == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode activity: %v", err)
			}
		}
		return w.Code, resp
	}

	code, first := read(t, "?limit=2")
	if code != http.StatusOK {
		t.Fatalf("first page: %d", code)
	}
	if len(first.Entries) != 2 || first.Entries[0].Action != "priority_changed" || first.Entries[1].Action != "status_changed" {
		t.Fatalf("first page = %+v, want priority then status, newest first", first.Entries)
	}
	if e := first.Entries[0]; e.IssueID != issueID || e.IssueTitle != "activity issue" || e.IssueStatus != "in_progress" ||
		!strings.HasSuffix(e.IssueIdentifier, "-"+strconv.Itoa(number)) || e.ActorType != "member" || e.ActorID != testUserID {
		t.Errorf("entry = %+v, want the issue and its actor", e)
	}
	if first.NextCursor == nil {
		t.Fatal("next_cursor = nil, want a cursor for the remaining entry")
	}

	code, second := read(t, "?limit=2&before="+url.QueryEscape(*first.NextCursor))
	if code != http.StatusOK {
		t.Fatalf("second page: %d", code)
	}
	if len(second.Entries) != 1 || second.Entries[0].Action != "created" || second.NextCursor != nil {
		t.Fatalf("second page = %+v (cursor %v), want only created and no cursor", second.Entries, second.NextCursor)
	}

	if code, _ := read(t, "?before=garbage"); code != http.StatusBadRequest {
		t.Errorf("malformed cursor: %d, want 400", code)
	}
	if code, _ := read(t, "?limit=0"); code != http.StatusBadRequest {
		t.Errorf("zero limit: %d, want 400", code)
	}

	w = httptest.NewRecorder()
	missing := "00000000-0000-0000-0000-000000000000"
	testHandler.ListProjectActivity(w, withURLParam(newRequest("GET", "/api/projects/"+missing+"/activity", nil), "id", missing))
	if w.Code != http.StatusNotFound {
		t.Errorf("missing project: %d, want 404", w.Code)
	}
}
