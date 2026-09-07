package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRunWorkspaceSourcePolicyValidation(t *testing.T) {
	for _, raw := range []string{
		`{"local_path":"/tmp/source","daemon_id":"host","execution_mode":"run_owned"}`,
		`{"local_path":"/tmp/source","daemon_id":"host","execution_mode":"run_owned","inherit_workspace_repositories":true}`,
		`{"local_path":"/tmp/source","daemon_id":"host","execution_mode":"run_owned","inherit_workspace_repositories":false,"base_commit":"main"}`,
	} {
		if _, err := validateLocalDirectoryRef(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsafe policy accepted: %s", raw)
		}
	}
	raw := json.RawMessage(`{"local_path":"/tmp/source","daemon_id":"host","execution_mode":"run_owned","inherit_workspace_repositories":false}`)
	if _, err := validateLocalDirectoryRef(raw); err != nil {
		t.Fatal(err)
	}
	resources := []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: raw}}
	runtime := db.AgentRuntime{DaemonID: pgtype.Text{String: "host", Valid: true}}
	if worktreeClaimBlockReason(resources, runtime, true, false) == "" {
		t.Fatal("old worktree capability incorrectly granted run-owned execution")
	}
	if reason := worktreeClaimBlockReason(resources, runtime, true, true); reason != "" {
		t.Fatal(reason)
	}
}

func TestRunWorkspaceExplicitEmptyRepositories(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setHandlerTestWorkspaceRepos(t, []map[string]string{{"url": localFallbackRepoURL}})
	project := dbfx.Project(t, "Operations scratch")
	dbfx.Insert(t, "project_resource", testutil.Cols{"workspace_id": testWorkspaceID, "project_id": project, "resource_type": "local_directory", "resource_ref": `{"daemon_id":"host","local_path":"/tmp/knowledge","execution_mode":"run_owned","inherit_workspace_repositories":false}`})
	result, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(project), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repos) != 0 {
		t.Fatalf("workspace repo leaked into explicit empty policy: %+v", result.Repos)
	}
	dbfx.Insert(t, "project_resource", testutil.Cols{"workspace_id": testWorkspaceID, "project_id": project, "resource_type": "github_repo", "resource_ref": `{"url":"https://github.com/example/approved","ref":"accepted"}`})
	result, err = testHandler.resolveClaimProjectContext(context.Background(), parseUUID(project), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repos) != 1 || result.Repos[0].URL != "https://github.com/example/approved" {
		t.Fatalf("explicit sources not respected: %+v", result.Repos)
	}
}
