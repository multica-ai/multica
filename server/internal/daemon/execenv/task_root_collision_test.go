package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// APEX-1692 / APEX-1684 vs APEX-1685: two issues created ~2s apart share the
// first eight UUIDv7 characters. They belong to different projects.
const (
	apex1684IssueID = "01a02554-9110-75ad-9f58-3c51f673ee8f"
	apex1685IssueID = "01a02554-9a14-7ea7-86bf-63ad1dbace8d"
	commandCenterID = "949398e5-2e0e-4584-8d3c-98c0439c79c4"
	houseBuilderID  = "63be9f70-ea4d-435b-a099-3c53b4cba706"
)

func TestPredictRootDirDistinctForAPEX1684And1685(t *testing.T) {
	t.Parallel()
	a := PredictRootDir("/root", "ws", apex1684IssueID)
	b := PredictRootDir("/root", "ws", apex1685IssueID)
	if a == b {
		t.Fatalf("APEX-1684 and APEX-1685 share env root %q", a)
	}
	if strings.HasSuffix(a, "01a02554") || strings.HasSuffix(b, "01a02554") {
		t.Fatalf("env root still uses the shared 8-char prefix: %q %q", a, b)
	}
}

func TestPrepareDistinctRootsAndContextsForSharedPrefixCrossProject(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	envCC, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-collision",
		TaskID:         apex1684IssueID,
		AgentName:      "Codex Studio",
		Task: TaskContextForEnv{
			IssueID:   apex1684IssueID,
			AgentID:   "agent-cc",
			ProjectID: commandCenterID,
			ProjectResources: []ProjectResourceForEnv{{
				ID:           "res-cc",
				ResourceType: "github_repo",
				ResourceRef:  json.RawMessage(`{"url":"https://github.com/apexagi-app/command-center.git"}`),
			}},
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare Command Center: %v", err)
	}
	defer envCC.Cleanup(true)

	envHBP, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-collision",
		TaskID:         apex1685IssueID,
		AgentName:      "Grok HBP",
		Task: TaskContextForEnv{
			IssueID:   apex1685IssueID,
			AgentID:   "agent-hbp",
			ProjectID: houseBuilderID,
			ProjectResources: []ProjectResourceForEnv{{
				ID:           "res-hbp",
				ResourceType: "github_repo",
				ResourceRef:  json.RawMessage(`{"url":"https://github.com/example/housebuilderpro.git"}`),
			}},
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare House Builder Pro: %v", err)
	}
	defer envHBP.Cleanup(true)

	if envCC.RootDir == envHBP.RootDir {
		t.Fatalf("both projects share env root %q", envCC.RootDir)
	}

	assertIssueAndProject(t, envCC.WorkDir, apex1684IssueID, commandCenterID, "command-center")
	assertIssueAndProject(t, envHBP.WorkDir, apex1685IssueID, houseBuilderID, "housebuilderpro")

	ccProv, err := ReadManagedEnvProvenance(envCC.RootDir)
	if err != nil {
		t.Fatalf("cc provenance: %v", err)
	}
	hbpProv, err := ReadManagedEnvProvenance(envHBP.RootDir)
	if err != nil {
		t.Fatalf("hbp provenance: %v", err)
	}
	if ProvenanceMatchesBinding(ccProv, houseBuilderID, BindingRepoURLs(nil, []ProjectResourceForEnv{{
		ResourceType: "github_repo",
		ResourceRef:  json.RawMessage(`{"url":"https://github.com/example/housebuilderpro.git"}`),
	}})) {
		t.Fatal("Command Center provenance matched a House Builder Pro binding")
	}
	if ProvenanceMatchesBinding(hbpProv, commandCenterID, BindingRepoURLs(nil, []ProjectResourceForEnv{{
		ResourceType: "github_repo",
		ResourceRef:  json.RawMessage(`{"url":"https://github.com/apexagi-app/command-center.git"}`),
	}})) {
		t.Fatal("House Builder Pro provenance matched a Command Center binding")
	}
}

func TestWriteContextFilesRefreshingReplacesCrossProjectLeftovers(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(workDir, ".agent_context"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".multica", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".agent_context", "issue_context.md"), []byte("# Task Assignment\n\n**Issue ID:** "+apex1685IssueID+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleResources := `{"project_id":"` + houseBuilderID + `","project_title":"House Builder Pro","resources":[]}`
	if err := os.WriteFile(filepath.Join(workDir, ".multica", "project", "resources.json"), []byte(staleResources), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeContextFilesRefreshing(workDir, "codex", TaskContextForEnv{
		IssueID:      apex1684IssueID,
		AgentID:      "agent-cc",
		ProjectID:    commandCenterID,
		ProjectTitle: "Command Center",
		ProjectResources: []ProjectResourceForEnv{{
			ID:           "res-cc",
			ResourceType: "github_repo",
			ResourceRef:  json.RawMessage(`{"url":"https://github.com/apexagi-app/command-center.git"}`),
		}},
	}, &sidecarManifest{})
	if err != nil {
		t.Fatalf("writeContextFilesRefreshing: %v", err)
	}

	assertIssueAndProject(t, workDir, apex1684IssueID, commandCenterID, "command-center")
}

func TestProvenanceMatchesBinding(t *testing.T) {
	t.Parallel()
	p := &ManagedEnvProvenance{
		ProjectID: commandCenterID,
		RepoURLs:  []string{"https://github.com/apexagi-app/command-center"},
	}
	cc := BindingRepoURLs(nil, []ProjectResourceForEnv{{
		ResourceType: "github_repo",
		ResourceRef:  json.RawMessage(`{"url":"https://github.com/apexagi-app/command-center.git"}`),
	}})
	hbp := BindingRepoURLs(nil, []ProjectResourceForEnv{{
		ResourceType: "github_repo",
		ResourceRef:  json.RawMessage(`{"url":"https://github.com/example/housebuilderpro.git"}`),
	}})
	if !ProvenanceMatchesBinding(p, commandCenterID, cc) {
		t.Fatal("same project and repo should match")
	}
	if ProvenanceMatchesBinding(p, houseBuilderID, hbp) {
		t.Fatal("cross-project binding must fail closed")
	}
	legacy := &ManagedEnvProvenance{ProjectID: ""}
	if !ProvenanceMatchesBinding(legacy, commandCenterID, cc) {
		t.Fatal("legacy provenance without project must still be reusable")
	}
}

func assertIssueAndProject(t *testing.T, workDir, issueID, projectID, repoNeedle string) {
	t.Helper()
	ctxBytes, err := os.ReadFile(filepath.Join(workDir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("issue_context.md: %v", err)
	}
	if !strings.Contains(string(ctxBytes), issueID) {
		t.Fatalf("issue_context.md does not name %s:\n%s", issueID, ctxBytes)
	}
	resBytes, err := os.ReadFile(filepath.Join(workDir, ".multica", "project", "resources.json"))
	if err != nil {
		t.Fatalf("resources.json: %v", err)
	}
	if !strings.Contains(string(resBytes), projectID) {
		t.Fatalf("resources.json does not name project %s:\n%s", projectID, resBytes)
	}
	if !strings.Contains(string(resBytes), repoNeedle) {
		t.Fatalf("resources.json does not name repo %s:\n%s", repoNeedle, resBytes)
	}
	markerBytes, err := os.ReadFile(filepath.Join(workDir, TaskContextMarkerRelPath))
	if err != nil {
		t.Fatalf("daemon_task_context.json: %v", err)
	}
	if !strings.Contains(string(markerBytes), issueID) {
		t.Fatalf("task context marker does not name %s:\n%s", issueID, markerBytes)
	}
}
