package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListIssueDependenciesKeepsWorkspaceBoundary(t *testing.T) {
	first := dbfx.Issue(t, "dependency source")
	second := dbfx.Issue(t, "dependency target")
	foreignWorkspace := dbfx.Workspace(t, "Foreign dependency workspace", "foreign-dependency-workspace")
	foreign := dbfx.Issue(t, "foreign dependency issue", testutil.Cols{
		"workspace_id": foreignWorkspace,
	})
	dbfx.Insert(t, "issue_dependency", testutil.Cols{
		"issue_id": first, "depends_on_issue_id": second, "type": "blocked_by",
	})
	dbfx.Insert(t, "issue_dependency", testutil.Cols{
		"issue_id": first, "depends_on_issue_id": foreign, "type": "blocks",
	})

	var response struct {
		Complete     bool                      `json:"complete"`
		Dependencies []issueDependencyResponse `json:"dependencies"`
	}
	testutil.Call(t, testHandler.ListIssueDependencies,
		newRequest(http.MethodGet, "/api/issues/dependencies", nil),
	).Want(http.StatusOK).JSON(&response)
	if !response.Complete {
		t.Fatal("dependency response is not marked complete")
	}
	var matched int
	for _, edge := range response.Dependencies {
		if edge.IssueID == foreign || edge.DependsOnIssueID == foreign {
			t.Fatalf("foreign issue leaked through dependency endpoint: %+v", edge)
		}
		if edge.IssueID == first && edge.DependsOnIssueID == second && edge.Type == "blocked_by" {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("matching dependency count = %d, want 1", matched)
	}
	foreignRequest := newRequest(http.MethodGet, "/api/issues/dependencies", nil)
	foreignRequest.Header.Set("X-Workspace-ID", foreignWorkspace)
	testutil.Call(t, testHandler.ListIssueDependencies, foreignRequest).Want(http.StatusNotFound)
}
