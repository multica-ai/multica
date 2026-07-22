package execenv

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
		Task:           TaskContextForEnv{IssueID: "issue-helper-reuse"},
	}
	env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}
	reused, err := ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot,
		WorkDir:        env.WorkDir,
		Provider:       params.Provider,
		Task: TaskContextForEnv{
			IssueID:         "issue-helper-reuse",
			NewCommentCount: 1,
			ProjectID:       "project-helper-reuse",
			ProjectResources: []ProjectResourceForEnv{
				{
					ID:           "resource-helper-reuse",
					ResourceType: "github_repo",
					ResourceRef:  json.RawMessage(`{"url":"https://github.com/multica-ai/multica"}`),
				},
			},
		},
	}, logger)
	if err != nil {
		t.Fatalf("ReuseIsolated: %v", err)
	}
	if reused == nil || reused.RootDir != env.RootDir || reused.WorkDir != env.WorkDir {
		t.Fatalf("reused environment = %#v, want root %q workdir %q", reused, env.RootDir, env.WorkDir)
	}
}

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
			IssueID:   "issue-helper-project-resource",
			ProjectID: "project-helper-project-resource",
			ProjectResources: []ProjectResourceForEnv{
				{
					ID:           "resource-helper-project-resource",
					ResourceType: "github_repo",
					ResourceRef:  json.RawMessage(`{"url":"https://github.com/multica-ai/multica"}`),
					Label:        "Multica",
				},
			},
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
	resource := got.Resources[0]
	var ref struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(resource.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource ref: %v", err)
	}
	if resource.ID != "resource-helper-project-resource" ||
		resource.ResourceType != "github_repo" ||
		ref.URL != "https://github.com/multica-ai/multica" ||
		resource.Label != "Multica" {
		t.Fatalf("project resource = %#v, want all fields preserved", resource)
	}
}

func TestPreparationRequestPreservesOpenclawGatewayForHelper(t *testing.T) {
	t.Parallel()
	want := OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "real-secret",
		TLS:   true,
	}
	tests := []struct {
		name    string
		request preparationRequest
		pin     func(preparationRequest) OpenclawGatewayPin
	}{
		{
			name: "prepare",
			request: preparationRequest{
				Action:  preparationActionPrepare,
				Prepare: &PrepareParams{OpenclawGateway: want},
			},
			pin: func(request preparationRequest) OpenclawGatewayPin {
				return request.Prepare.OpenclawGateway
			},
		},
		{
			name: "reuse",
			request: preparationRequest{
				Action: preparationActionReuse,
				Reuse:  &ReuseParams{OpenclawGateway: want},
			},
			pin: func(request preparationRequest) OpenclawGatewayPin {
				return request.Reuse.OpenclawGateway
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redacted, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("marshal redacted request: %v", err)
			}
			if bytes.Contains(redacted, []byte(want.Token)) {
				t.Fatalf("ordinary JSON leaked gateway token: %s", redacted)
			}

			payload, err := marshalPreparationRequest(tt.request)
			if err != nil {
				t.Fatalf("marshal preparation request: %v", err)
			}
			got, err := decodePreparationRequest(bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("decode preparation request: %v", err)
			}
			if got := tt.pin(got); got != want {
				t.Fatal("gateway pin fields did not survive the helper protocol")
			}
		})
	}
}
