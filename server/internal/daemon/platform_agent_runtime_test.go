package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunTaskPlatformAgentMaterializesClaimContextAndIdentity drives the real
// daemon launch path with a test-created app-server executable. It catches a
// dropped raw runtime_config field, missing sidecar plumbing, wrong skill
// location, or any omitted Multica identity variable without resolving an
// ambient user CLI.
func TestRunTaskPlatformAgentMaterializesClaimContextAndIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake app-server fixture")
	}
	testDir := t.TempDir()
	capturedEnv := filepath.Join(testDir, "runtime.env")
	capturedContext := filepath.Join(testDir, "context.json")
	fakeCLI := filepath.Join(testDir, "platform-agent-cli")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'platform-agent-cli 0.1.0'
  exit 0
fi
printf 'MULTICA_WORKSPACE_ID=%s\nMULTICA_AGENT_ID=%s\nMULTICA_TASK_ID=%s\nMULTICA_RUNTIME_ID=%s\n' \
  "$MULTICA_WORKSPACE_ID" "$MULTICA_AGENT_ID" "$MULTICA_TASK_ID" "$MULTICA_RUNTIME_ID" > "$CAPTURE_ENV"
cp .platform-agent/context.json "$CAPTURE_CONTEXT"
test -f AGENTS.md || exit 21
test -f .agent_context/skills/source-review/SKILL.md || exit 22
IFS= read -r _
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}'
IFS= read -r _
IFS= read -r _
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-platform-daemon"}}}'
IFS= read -r _
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-platform-daemon","turn":{"id":"turn-platform-daemon","status":"completed"}}}'
`
	if err := os.WriteFile(fakeCLI, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"runtime-platform": {ID: "runtime-platform", Provider: "platform-agent-cli"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"platform-agent-cli": {Path: fakeCLI},
			},
		},
	}
	task := Task{
		ID:          "task-platform",
		WorkspaceID: "workspace-platform",
		RuntimeID:   "runtime-platform",
		IssueID:     "issue-platform",
		AgentID:     "agent-platform",
		AuthToken:   "mat_platform_test",
		Agent: &AgentData{
			ID:   "agent-platform",
			Name: "Platform Lead",
			Skills: []SkillData{{
				ID:      "skill-source-review",
				Name:    "Source Review",
				Content: "---\nname: source-review\n---\n\nReview sources.",
			}},
			CustomEnv: map[string]string{
				"CAPTURE_ENV":     capturedEnv,
				"CAPTURE_CONTEXT": capturedContext,
			},
			RuntimeConfig: []byte(`{"platform_agent":` + validPlatformAgentPayload() + `}`),
		},
	}

	result, err := d.runTask(context.Background(), task, "platform-agent-cli", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("runTask() result = %+v", result)
	}
	envData, err := os.ReadFile(capturedEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"MULTICA_WORKSPACE_ID=workspace-platform",
		"MULTICA_AGENT_ID=agent-platform",
		"MULTICA_TASK_ID=task-platform",
		"MULTICA_RUNTIME_ID=runtime-platform",
	} {
		if !strings.Contains(string(envData), expected) {
			t.Errorf("child environment missing %q:\n%s", expected, envData)
		}
	}
	contextData, err := os.ReadFile(capturedContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"key": "research-team"`, `"source_key": "lead-researcher"`, `"name": "summarize"`} {
		if !strings.Contains(string(contextData), expected) {
			t.Errorf("captured context missing %q:\n%s", expected, contextData)
		}
	}
}

func TestTaskIdentityEnvironmentScopesRuntimeIDToPlatformProvider(t *testing.T) {
	task := Task{
		ID:          "task-identity",
		WorkspaceID: "workspace-identity",
		AgentID:     "agent-identity",
		RuntimeID:   "runtime-identity",
	}

	platform := taskIdentityEnvironment(task, "platform-agent-cli")
	if got := platform["MULTICA_RUNTIME_ID"]; got != "runtime-identity" {
		t.Fatalf("platform MULTICA_RUNTIME_ID = %q, want runtime-identity", got)
	}
	for key, want := range map[string]string{
		"MULTICA_WORKSPACE_ID": "workspace-identity",
		"MULTICA_AGENT_ID":     "agent-identity",
		"MULTICA_TASK_ID":      "task-identity",
	} {
		if got := platform[key]; got != want {
			t.Fatalf("platform %s = %q, want %q", key, got, want)
		}
	}

	other := taskIdentityEnvironment(task, "claude")
	if len(other) != 3 {
		t.Fatalf("non-platform identity environment = %#v, want exactly three existing identity keys", other)
	}
	if _, ok := other["MULTICA_RUNTIME_ID"]; ok {
		t.Fatalf("non-platform environment leaked MULTICA_RUNTIME_ID: %#v", other)
	}
	for key, want := range map[string]string{
		"MULTICA_WORKSPACE_ID": "workspace-identity",
		"MULTICA_AGENT_ID":     "agent-identity",
		"MULTICA_TASK_ID":      "task-identity",
	} {
		if got := other[key]; got != want {
			t.Fatalf("non-platform %s = %q, want %q", key, got, want)
		}
	}
}
