package execenv

// CEREBRO-PATCH(mul-4923-prepare-timeout): backport of upstream MUL-4923 (#5584); drop on next upstream sync.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const preparationHelperTestMode = "execenv-preparation-helper"

func preparationHelperTestCommand() []string {
	return []string{
		os.Args[0],
		"-test.run=^TestPreparationHelperProcess$",
		"--",
		preparationHelperTestMode,
	}
}

// TestPreparationHelperProcess is both a no-op parent-side test and the child
// entry point used by isolation tests. Keeping it in the package test binary
// exercises the same stdin/stdout protocol as the real multica helper.
func TestPreparationHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != preparationHelperTestMode {
		return
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPreparationHelperRoundTripsReuse(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-helper-reuse",
		TaskID:         "99999999-8888-7777-6666-555555555555",
		Provider:       "claude",
		// CEREBRO-PATCH(mul-5038-project-resource-decode): resources on both paths.
		Task: TaskContextForEnv{
			IssueID:          "issue-helper-reuse",
			ProjectID:        "project-helper-reuse",
			ProjectResources: []ProjectResourceForEnv{testProjectResource()},
		},
	}
	env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}
	reused, err := ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
		// Fork ReuseParams keys off WorkDir only (no WorkspacesRoot field).
		WorkDir:  env.WorkDir,
		Provider: params.Provider,
		// CEREBRO-PATCH(mul-5038-project-resource-decode): resources on the reuse path too.
		Task: TaskContextForEnv{
			IssueID:          "issue-helper-reuse",
			NewCommentCount:  1,
			ProjectID:        "project-helper-reuse",
			ProjectResources: []ProjectResourceForEnv{testProjectResource()},
		},
	}, logger)
	if err != nil {
		t.Fatalf("ReuseIsolated: %v", err)
	}
	if reused == nil || reused.RootDir != env.RootDir || reused.WorkDir != env.WorkDir {
		t.Fatalf("reused environment = %#v, want root %q workdir %q", reused, env.RootDir, env.WorkDir)
	}
}

// CEREBRO-PATCH(mul-5038-project-resource-decode): backport of upstream MUL-5038 (#5688); drop on next upstream sync.
// testProjectResource is the fixture that reproduces FIR-3801: the helper
// decodes the preparation request with DisallowUnknownFields, so a project
// resource in the payload used to abort every task before the agent started.
func testProjectResource() ProjectResourceForEnv {
	return ProjectResourceForEnv{
		ID:           "resource-helper-project",
		ResourceType: "github_repo",
		ResourceRef:  json.RawMessage(`{"url":"https://github.com/firtal-group/firtal-cerebro"}`),
		Label:        "firtal-cerebro",
	}
}

// TestPreparationHelperRoundTripsProjectResources locks the encode/decode names
// of ProjectResourceForEnv together across the helper process boundary. Drop a
// json tag and this test fails instead of production.
func TestPreparationHelperRoundTripsProjectResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-helper-project-resource",
		TaskID:         "88888888-7777-6666-5555-444444444444",
		Provider:       "claude",
		Task: TaskContextForEnv{
			IssueID:          "issue-helper-project-resource",
			ProjectID:        "project-helper-project-resource",
			ProjectResources: []ProjectResourceForEnv{testProjectResource()},
		},
	}

	env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}
	defer env.Cleanup(true)

	data, err := os.ReadFile(filepath.Join(env.WorkDir, ".multica", "project", "resources.json"))
	if err != nil {
		t.Fatalf("read project resources: %v", err)
	}
	var got projectResourceFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode project resources: %v", err)
	}
	if len(got.Resources) != 1 {
		t.Fatalf("project resources = %#v, want one resource", got.Resources)
	}
	want := testProjectResource()
	resource := got.Resources[0]
	var ref struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(resource.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource ref: %v", err)
	}
	if resource.ID != want.ID || resource.ResourceType != want.ResourceType ||
		resource.Label != want.Label || ref.URL != "https://github.com/firtal-group/firtal-cerebro" {
		t.Fatalf("project resource = %#v, want all fields preserved", resource)
	}
}

// CEREBRO-PATCH(execenv-session-mode-workflow-compat): regression coverage for mixed-version reuse.
func TestPreparationHelperAcceptsRetiredSessionModeWorkflowID(t *testing.T) {
	current, err := json.Marshal(TaskContextForEnv{})
	if err != nil {
		t.Fatalf("marshal current task context: %v", err)
	}
	if bytes.Contains(current, []byte(`"SessionModeWorkflowID"`)) {
		t.Fatalf("current task context still emits retired SessionModeWorkflowID: %s", current)
	}

	payload, err := json.Marshal(map[string]any{
		"action": "reuse",
		"reuse": map[string]any{
			"WorkDir":  t.TempDir(),
			"Provider": "claude",
			"Task": map[string]any{
				"SessionModeWorkflowID": "workflow-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy preparation request: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer
	if err := RunPreparationHelper(bytes.NewReader(payload), &out, logger); err != nil {
		t.Fatalf("RunPreparationHelper rejected legacy SessionModeWorkflowID: %v", err)
	}
}
