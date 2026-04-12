package daemon

import (
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestBuildPromptContainsIssueContextFileHint(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Issue: &IssueData{
			Identifier: "FEL-8",
			Title:      "Fix athlete-truth answer action review blockers",
		},
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	for _, want := range []string{
		issueID,
		"FEL-8",
		"Fix athlete-truth answer action review blockers",
		".agent_context/issue_context.md",
		"do not depend on `multica issue get`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptWithoutIssueDetailsStillUsesInjectedContext(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	if !strings.Contains(prompt, ".agent_context/issue_context.md") {
		t.Fatal("prompt should send the agent to injected issue context")
	}
	if strings.Contains(prompt, "Start by running `multica issue get") {
		t.Fatal("prompt should not require CLI issue fetch as the first step")
	}
}

func TestDetectBranchNameFindsCheckedOutRepoBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")
	runGitTestCommand(t, workDir, "init", "repo")
	runGitTestCommand(t, repoDir, "checkout", "-b", "agent/fix/writeback")

	if got := detectBranchName(workDir); got != "agent/fix/writeback" {
		t.Fatalf("detectBranchName() = %q, want agent/fix/writeback", got)
	}
}

func TestDetectBranchNameIsBounded(t *testing.T) {
	workDir := t.TempDir()
	oldReadBranchDirEntries := readBranchDirEntries
	t.Cleanup(func() {
		readBranchDirEntries = oldReadBranchDirEntries
	})

	requestedN := 0
	readBranchDirEntries = func(name string, n int) ([]os.DirEntry, error) {
		requestedN = n
		entries := make([]os.DirEntry, branchDetectionMaxCandidates+5)
		for i := range entries {
			entries[i] = fakeDirEntry{name: "dir-" + string(rune('a'+i)), isDir: true}
		}
		return entries, nil
	}

	if got := detectBranchName(workDir); got != "" {
		t.Fatalf("detectBranchName() = %q, want empty because fake entries are not git repos", got)
	}
	if requestedN != branchDetectionMaxCandidates-1 {
		t.Fatalf("ReadDir requested %d entries, want %d", requestedN, branchDetectionMaxCandidates-1)
	}
}

type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f fakeDirEntry) Name() string {
	return f.name
}

func (f fakeDirEntry) IsDir() bool {
	return f.isDir
}

func (f fakeDirEntry) Type() fs.FileMode {
	if f.isDir {
		return fs.ModeDir
	}
	return 0
}

func (f fakeDirEntry) Info() (fs.FileInfo, error) {
	return nil, nil
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}
