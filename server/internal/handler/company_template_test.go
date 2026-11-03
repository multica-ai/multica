package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// companyTemplateEnv is a workspace of its own with one runtime, owned by the
// test user, so a test sees only what its own apply made.
type companyTemplateEnv struct {
	WorkspaceID string
	RuntimeID   string
}

func newCompanyTemplateEnv(t *testing.T) companyTemplateEnv {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Company "+uuid.NewString(), "company-"+uuid.NewString(), testutil.Cols{"issue_prefix": "CMP"})
	dbfx.Member(t, ws, testUserID, "owner")
	rt := dbfx.Runtime(t, "Worker "+uuid.NewString(), testutil.Cols{"workspace_id": ws, "status": "online"})
	t.Cleanup(func() {
		ctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM squad_routine WHERE workspace_id = $1`,
			`DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE workspace_id = $1)`,
			`DELETE FROM autopilot WHERE workspace_id = $1`,
			`DELETE FROM squad_charter WHERE workspace_id = $1`,
			`DELETE FROM pipeline WHERE workspace_id = $1`,
			`DELETE FROM office_floor WHERE workspace_id = $1`,
		} {
			testPool.Exec(ctx, stmt, ws)
		}
	})
	return companyTemplateEnv{WorkspaceID: ws, RuntimeID: rt}
}

func (e companyTemplateEnv) apply(t *testing.T, userID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest("POST", "/api/office/company-template/apply", body)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if userID != "" {
		req = asUser(req, userID)
	}
	return testutil.Call(t, testHandler.ApplyCompanyTemplate, req)
}

func TestApplyCompanyTemplateRefusesAMemberAndBadRequests(t *testing.T) {
	e := newCompanyTemplateEnv(t)
	plain := dbfx.User(t, "Plain member "+uuid.NewString(), "company-member-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, e.WorkspaceID, plain, "member")

	body := map[string]any{"runtime_id": e.RuntimeID, "squads": []string{"planning"}, "start": "leave_paused"}
	e.apply(t, plain, body).Want(http.StatusForbidden)

	e.apply(t, testUserID, map[string]any{"runtime_id": "not-a-uuid", "squads": []string{"planning"}, "start": "leave_paused"}).Want(http.StatusBadRequest)
	e.apply(t, testUserID, map[string]any{"runtime_id": uuid.NewString(), "squads": []string{"planning"}, "start": "leave_paused"}).Want(http.StatusBadRequest)
	e.apply(t, testUserID, map[string]any{"runtime_id": e.RuntimeID, "squads": []string{"no_such_squad"}, "start": "leave_paused"}).Want(http.StatusBadRequest)
	e.apply(t, testUserID, map[string]any{"runtime_id": e.RuntimeID, "squads": []string{}, "start": "leave_paused"}).Want(http.StatusBadRequest)
	e.apply(t, testUserID, map[string]any{"runtime_id": e.RuntimeID, "squads": []string{"planning"}, "start": "sometime"}).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM squad WHERE workspace_id = $1`, e.WorkspaceID); n != 0 {
		t.Fatalf("refused requests created %d squads", n)
	}
}

func TestApplyCompanyTemplateReportsEachStep(t *testing.T) {
	e := newCompanyTemplateEnv(t)
	// The Planning squad needs no Secretary. The deployment in tests does not
	// offer knowledge, so the knowledge step is reported as failed and the rest
	// of the company is installed.
	res := e.apply(t, testUserID, map[string]any{
		"runtime_id": e.RuntimeID, "model": "test-model", "squads": []string{"planning"}, "start": "leave_paused",
	}).Want(http.StatusOK).Map()

	steps, ok := res["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("steps = %v", res["steps"])
	}
	statuses := map[string]string{}
	for _, s := range steps {
		step := s.(map[string]any)
		statuses[step["key"].(string)] = step["status"].(string)
	}
	if statuses["squad:planning"] != "created" {
		t.Fatalf("squad:planning = %q, want created; steps %v", statuses["squad:planning"], steps)
	}
	if statuses["pipeline"] != "created" {
		t.Fatalf("pipeline = %q, want created", statuses["pipeline"])
	}
	if statuses["knowledge:ventures"] != "failed" {
		t.Fatalf("knowledge:ventures = %q, want failed on a deployment without knowledge", statuses["knowledge:ventures"])
	}
	if res["pipeline_id"] == nil {
		t.Fatal("pipeline_id is null though the pipeline was installed")
	}
	unowned, ok := res["unowned_stages"].([]any)
	if !ok || len(unowned) != 6 {
		t.Fatalf("unowned_stages = %v, want the six stages Planning does not own", res["unowned_stages"])
	}
}
