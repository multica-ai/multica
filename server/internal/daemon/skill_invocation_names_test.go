package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const skillInvocationHelperMode = "skill-invocation-helper"

// TestSkillInvocationHelperProcess runs the preparation protocol or a fake
// Claude process from this test binary, including on Windows. No installed
// agent executable or authenticated account is consulted.
func TestSkillInvocationHelperProcess(t *testing.T) {
	if len(os.Args) < 5 || os.Args[3] != skillInvocationHelperMode {
		return
	}
	if os.Args[4] == "prepare" {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	brief, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[4], brief, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(`{"type":"system","session_id":"session-skill-collision"}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"session-skill-collision","result":"done"}`)
	os.Exit(0)
}

func TestRunTaskListsAllocatedSkillInvocationName(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	userSkillPath := filepath.Join(workDir, ".claude", "skills", "review", "SKILL.md")
	const userSkill = "---\nname: review\n---\n\nUser-owned review skill.\n"
	if err := os.MkdirAll(filepath.Dir(userSkillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSkillPath, []byte(userSkill), 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := json.Marshal(localDirectoryRef{
		LocalPath: workDir, DaemonID: "daemon-skill-collision", ExecutionMode: localDirectoryModeInPlace,
	})
	if err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(t.TempDir(), "runtime-brief.md")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prefix := []string{"-test.run=^TestSkillInvocationHelperProcess$", "--", skillInvocationHelperMode}
	d := &Daemon{
		client: NewClient(srv.URL), logger: logger,
		workspaces: make(map[string]*workspaceState),
		runtimeIndex: map[string]Runtime{
			"runtime-skill-collision": {ID: "runtime-skill-collision", Provider: "claude", ProfileID: "fake-claude"},
		},
		profileLaunchSpecs: map[string]profileLaunchSpec{
			"fake-claude": {path: os.Args[0], version: "test", fixedArgs: append(append([]string{}, prefix...), capturePath)},
		},
		activeEnvRoots: make(map[string]int),
		executionEnvironmentCommand: func() ([]string, error) {
			return append(append([]string{os.Args[0]}, prefix...), "prepare"), nil
		},
		cfg: Config{
			DaemonID: "daemon-skill-collision", WorkspacesRoot: t.TempDir(),
			AgentTimeout: 15 * time.Second, ServerBaseURL: srv.URL,
		},
	}
	task := Task{
		ID: "task-skill-collision", WorkspaceID: "workspace-skill-collision",
		RuntimeID: "runtime-skill-collision", IssueID: "issue-skill-collision", AgentID: "agent-skill-collision",
		AuthToken:        "mat_skill_collision_test",
		ProjectResources: []ProjectResourceData{{ID: "local-resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}},
		Agent: &AgentData{
			ID: "agent-skill-collision", Name: "test-agent",
			Skills: []SkillData{{ID: "assigned-review", Source: "workspace", Name: "Review", Content: "---\nname: original-review\n---\n\nAssigned review skill.\n"}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := d.runTask(ctx, task, "claude", 0, logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.SessionID != "session-skill-collision" {
		t.Fatalf("fake agent did not complete: %+v", result)
	}
	brief, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured runtime brief: %v", err)
	}
	if !strings.Contains(string(brief), "**review-multica**") || strings.Contains(string(brief), "**review**") {
		t.Errorf("runtime brief must list the allocated skill name review-multica, not the user-owned review:\n%s", brief)
	}
	if got, err := os.ReadFile(userSkillPath); err != nil || string(got) != userSkill {
		t.Fatalf("user-owned skill changed: %q, %v", got, err)
	}
}
