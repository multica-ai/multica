package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// assignSquadProject posts the assignment as the workspace admin.
func assignSquadProject(t *testing.T, fx iniWorkspace, squadID, projectID string, body map[string]any) *testutil.Response {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	body["project_id"] = projectID
	req := newRequestAs(fx.AdminID, "POST", "/api/squads/"+squadID+"/project", body)
	req = withURLParams(req, "workspaceId", fx.ID, "id", squadID)
	return testutil.Call(t, testHandler.AssignSquadProject, req)
}

// TestAssignSquadProjectSetsSDLCWorkflowAndSquad: the project takes the squad's
// SDLC Auto workflow and shows on the squad's room.
func TestAssignSquadProjectSetsSDLCWorkflowAndSquad(t *testing.T) {
	fx := newIniWorkspace(t)
	projectID := dbfxProject(t, fx, "Mua Ngon Ecommerce")

	var out struct {
		Project struct {
			ID      string  `json:"id"`
			SquadID *string `json:"squad_id"`
		} `json:"project"`
	}
	assignSquadProject(t, fx, fx.Build, projectID, nil).Want(http.StatusOK).JSON(&out)
	if out.Project.SquadID == nil || *out.Project.SquadID != fx.Build {
		t.Fatalf("project squad_id = %v, want %s", out.Project.SquadID, fx.Build)
	}
	if got := projectWorkflowName(t, projectID); got != "SDLC Auto" {
		t.Fatalf("project workflow = %q, want SDLC Auto", got)
	}
}

// TestAssignSquadProjectUnassignsPreviousProject: a squad holds one project.
func TestAssignSquadProjectUnassignsPreviousProject(t *testing.T) {
	fx := newIniWorkspace(t)
	first := dbfxProject(t, fx, "First")
	second := dbfxProject(t, fx, "Second")
	assignSquadProject(t, fx, fx.Build, first, nil).Want(http.StatusOK)
	assignSquadProject(t, fx, fx.Build, second, nil).Want(http.StatusOK)

	var squadID *string
	dbfx.QueryRow(t, `SELECT squad_id::text FROM project WHERE id = $1`, first).Scan(&squadID)
	if squadID != nil {
		t.Fatalf("previous project still assigned to %s", *squadID)
	}
}

// TestAssignSquadProjectNeedsSDLCSquad: a squad without the SDLC agents cannot build.
func TestAssignSquadProjectNeedsSDLCSquad(t *testing.T) {
	fx := newIniWorkspace(t)
	projectID := dbfxProject(t, fx, "Plain")
	assignSquadProject(t, fx, fx.Product, projectID, nil).Want(http.StatusUnprocessableEntity)
}

// TestAssignSquadProjectRefusesProjectOnAnotherWorkflow: a project already on a
// different workflow is changed from its settings, not by the assignment.
func TestAssignSquadProjectRefusesProjectOnAnotherWorkflow(t *testing.T) {
	fx := newIniWorkspace(t)
	projectID := dbfxProject(t, fx, "Custom flow")
	dbfx.Exec(t, `UPDATE project SET workflow_id = gen_random_uuid() WHERE id = $1`, projectID)
	assignSquadProject(t, fx, fx.Build, projectID, nil).Want(http.StatusConflict)
}

func dbfxProject(t *testing.T, fx iniWorkspace, title string) string {
	t.Helper()
	return dbfx.Project(t, title+" "+fx.ID[:8], testutil.Cols{"workspace_id": fx.ID})
}

func projectWorkflowName(t *testing.T, projectID string) string {
	t.Helper()
	var name *string
	dbfx.QueryRow(t, `SELECT w.name FROM project p JOIN issue_workflow w ON w.id = p.workflow_id WHERE p.id = $1`, projectID).Scan(&name)
	if name == nil {
		return ""
	}
	return *name
}
