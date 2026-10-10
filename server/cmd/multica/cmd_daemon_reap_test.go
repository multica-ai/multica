package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func newWorkspaceReapTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.Bool("apply", false, "")
	f.String("output", "table", "")
	f.String("workspaces-root", "", "")
	f.String("profile", "", "")
	f.String("server-url", "", "")
	return cmd
}

func TestRunDaemonReapWorkspacesDryRunRequiresApply(t *testing.T) {
	pinHumanCLIContext(t)
	home := t.TempDir()
	root := filepath.Join(home, "reap-root")
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_SERVER_URL", "")

	recorder := &gcCheckRecorder{statuses: map[string]string{"issue-done": "done"}}
	server := newGCCheckServer(t, recorder)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "reap-token"}, ""); err != nil {
		t.Fatal(err)
	}
	taskRoot := writeReapIssueTask(t, root, "11111111-1111-1111-1111-111111111111", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "issue-done", false)

	cmd := newWorkspaceReapTestCmd(t)
	if err := cmd.Flags().Set("workspaces-root", root); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runDaemonReapWorkspaces(cmd, nil) })
	if err != nil {
		t.Fatalf("runDaemonReapWorkspaces dry-run: %v", err)
	}
	if _, err := os.Stat(taskRoot); err != nil {
		t.Fatalf("dry-run removed fixture task root: %v", err)
	}
	if !strings.Contains(out, "WOULD REMOVE "+taskRoot) {
		t.Fatalf("output did not report dry-run removal:\n%s", out)
	}
	if !strings.Contains(out, "Before: 1 task root(s)") || !strings.Contains(out, "After: 1 task root(s)") {
		t.Fatalf("output did not report before/after counts:\n%s", out)
	}
}

func TestRunDaemonReapWorkspacesApplyRemovesOnlyEligibleFixture(t *testing.T) {
	pinHumanCLIContext(t)
	home := t.TempDir()
	root := filepath.Join(home, "reap-root")
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_SERVER_URL", "")

	recorder := &gcCheckRecorder{statuses: map[string]string{"issue-cancelled": "cancelled"}}
	server := newGCCheckServer(t, recorder)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "reap-token"}, ""); err != nil {
		t.Fatal(err)
	}
	taskRoot := writeReapIssueTask(t, root, "22222222-2222-2222-2222-222222222222", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "issue-cancelled", false)

	cmd := newWorkspaceReapTestCmd(t)
	if err := cmd.Flags().Set("workspaces-root", root); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("apply", "true"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runDaemonReapWorkspaces(cmd, nil) })
	if err != nil {
		t.Fatalf("runDaemonReapWorkspaces apply: %v", err)
	}
	if _, err := os.Stat(taskRoot); !os.IsNotExist(err) {
		t.Fatalf("apply did not remove isolated fixture task root: stat err = %v", err)
	}
	if !strings.Contains(out, "REMOVED "+taskRoot) {
		t.Fatalf("output did not report removal:\n%s", out)
	}
	if !strings.Contains(out, "After: 0 task root(s)") {
		t.Fatalf("output did not report post-apply count:\n%s", out)
	}
}

func TestRunDaemonReapWorkspacesRefusesDirtyAndNonTerminalTrees(t *testing.T) {
	pinHumanCLIContext(t)
	home := t.TempDir()
	root := filepath.Join(home, "reap-root")
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_SERVER_URL", "")

	recorder := &gcCheckRecorder{statuses: map[string]string{
		"issue-dirty": "done",
		"issue-open":  "in_progress",
	}}
	server := newGCCheckServer(t, recorder)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "reap-token"}, ""); err != nil {
		t.Fatal(err)
	}
	dirtyRoot := writeReapIssueTask(t, root, "33333333-3333-3333-3333-333333333333", "cccccccc-cccc-cccc-cccc-cccccccccccc", "issue-dirty", true)
	openRoot := writeReapIssueTask(t, root, "44444444-4444-4444-4444-444444444444", "dddddddd-dddd-dddd-dddd-dddddddddddd", "issue-open", false)

	cmd := newWorkspaceReapTestCmd(t)
	if err := cmd.Flags().Set("workspaces-root", root); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("apply", "true"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runDaemonReapWorkspaces(cmd, nil) })
	if err != nil {
		t.Fatalf("runDaemonReapWorkspaces apply: %v", err)
	}
	for _, taskRoot := range []string{dirtyRoot, openRoot} {
		if _, err := os.Stat(taskRoot); err != nil {
			t.Fatalf("safety refusal removed %s: %v", taskRoot, err)
		}
	}
	if !strings.Contains(out, "Git tree has uncommitted or untracked files") || !strings.Contains(out, "untracked-deliverable.txt") {
		t.Fatalf("output did not name dirty-tree refusal and file:\n%s", out)
	}
	if !strings.Contains(out, "card status \"in_progress\" is not done or cancelled") {
		t.Fatalf("output did not name non-terminal-card refusal:\n%s", out)
	}
}

func writeReapIssueTask(t *testing.T, root, workspaceID, taskID, issueID string, dirty bool) string {
	t.Helper()
	taskRoot := filepath.Join(root, workspaceID, taskID)
	repo := filepath.Join(taskRoot, "workdir", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	owner, err := json.Marshal(map[string]string{"workspace_id": workspaceID, "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskRoot, ".task_owner"), owner, 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{
		"kind":         "issue",
		"issue_id":     issueID,
		"workspace_id": workspaceID,
		"completed_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskRoot, ".gc_meta.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "reaper-test@example.invalid")
	runGit(t, repo, "config", "user.name", "Reaper Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "fixture")
	if dirty {
		if err := os.WriteFile(filepath.Join(repo, "untracked-deliverable.txt"), []byte("do not delete"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return taskRoot
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
