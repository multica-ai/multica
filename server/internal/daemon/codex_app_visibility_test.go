package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCodexAppVisiblePortal(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "real-workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	task := Task{
		ID:          "task-1234567890",
		IssueID:     "issue-abcdef123456",
		WorkspaceID: "workspace-1",
		AgentID:     "agent-1",
		Agent:       &AgentData{Name: "Codex"},
	}
	result := TaskResult{
		Status:    "completed",
		Comment:   "done",
		SessionID: "thread-1",
		WorkDir:   workDir,
	}

	portal, err := writeCodexAppVisiblePortal(root, root, task, result)
	if err != nil {
		t.Fatalf("writeCodexAppVisiblePortal: %v", err)
	}
	if !strings.Contains(portal.Dir, "issue-issue-ab-task-task-12") {
		t.Fatalf("unexpected portal dir: %s", portal.Dir)
	}
	if filepath.IsAbs(portal.DisplayPath) {
		t.Fatalf("expected display path relative to visible cwd, got %s", portal.DisplayPath)
	}

	readme, err := os.ReadFile(portal.ReadmePath)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if !strings.Contains(string(readme), "Codex App-visible portal") {
		t.Fatalf("readme missing portal description:\n%s", readme)
	}

	resultMD, err := os.ReadFile(portal.ResultPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if !strings.Contains(string(resultMD), "done") {
		t.Fatalf("result missing final output:\n%s", resultMD)
	}

	linksData, err := os.ReadFile(portal.LinksPath)
	if err != nil {
		t.Fatalf("read links: %v", err)
	}
	var links map[string]string
	if err := json.Unmarshal(linksData, &links); err != nil {
		t.Fatalf("unmarshal links: %v", err)
	}
	if links["codex_session_id"] != "thread-1" || links["real_workdir"] != workDir {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestCodexAppVisiblePromptIsSingleLine(t *testing.T) {
	portal := codexAppVisiblePortal{Dir: "/tmp/portal"}
	got := codexAppVisiblePrompt(Task{ID: "task-1234", IssueID: "issue-1234"}, TaskResult{}, portal)
	if strings.Contains(got, "\n") {
		t.Fatalf("prompt should stay single-line for a compact Codex App title: %q", got)
	}
	if !strings.Contains(got, "[Multica]") || !strings.Contains(got, "/tmp/portal") {
		t.Fatalf("unexpected prompt: %q", got)
	}
}
