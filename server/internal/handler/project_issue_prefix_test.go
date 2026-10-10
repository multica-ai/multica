package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectIssuePrefixIssuesKeepImmutableIndependentSeries(t *testing.T) {
	createProject := func(title, prefix string, wantStatus int) ProjectResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "issue_prefix": prefix,
		})
		testHandler.CreateProject(w, req)
		if w.Code != wantStatus {
			t.Fatalf("CreateProject(%q, %q): got %d, want %d: %s", title, prefix, w.Code, wantStatus, w.Body.String())
		}
		var project ProjectResponse
		if wantStatus == http.StatusCreated {
			if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
				t.Fatalf("decode project: %v", err)
			}
		}
		return project
	}
	createIssue := func(title string, projectID *string) IssueResponse {
		t.Helper()
		body := map[string]any{"title": title, "status": "todo", "priority": "low"}
		if projectID != nil {
			body["project_id"] = *projectID
		}
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue(%q): got %d: %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		t.Cleanup(func() { deleteTestIssue(t, issue.ID) })
		return issue
	}

	project := createProject("prefix series project", "exp", http.StatusCreated)
	if project.IssuePrefix == nil || *project.IssuePrefix != "EXP" {
		t.Fatalf("project issue_prefix = %v, want EXP", project.IssuePrefix)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	first := createIssue("project prefix first", &project.ID)
	backlog := createIssue("workspace prefix control", nil)
	second := createIssue("project prefix second", &project.ID)
	if first.Identifier != "EXP-1" || second.Identifier != "EXP-2" {
		t.Fatalf("project identifiers = %q, %q; want EXP-1, EXP-2", first.Identifier, second.Identifier)
	}
	if strings.HasPrefix(backlog.Identifier, "EXP-") {
		t.Fatalf("workspace issue unexpectedly used project series: %q", backlog.Identifier)
	}

	// Moving an issue out of its project must not rewrite its issued key.
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET project_id = NULL WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("move issue to backlog: %v", err)
	}
	moved, err := testHandler.Queries.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{
		ID: util.MustParseUUID(first.ID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("reload moved issue: %v", err)
	}
	if moved.IdentifierPrefix != "EXP" || moved.Number != 1 {
		t.Fatalf("moved issue key = %s-%d, want EXP-1", moved.IdentifierPrefix, moved.Number)
	}

	// Changing the project prefix affects only future issues and starts a new series.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{"issue_prefix": "new"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject prefix: got %d: %s", w.Code, w.Body.String())
	}
	future := createIssue("new project prefix", &project.ID)
	if future.Identifier != "NEW-1" {
		t.Fatalf("future identifier = %q, want NEW-1", future.Identifier)
	}

	// EXP issued identifiers permanently reserve EXP for this workspace.
	createProject("must conflict with issued EXP", "EXP", http.StatusConflict)
}
