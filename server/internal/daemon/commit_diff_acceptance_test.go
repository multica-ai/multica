package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Drive actual task preparation, the provider adapter, Git finalization and
// message reporting. The CLI is a fixture: no installed agent or account runs.
func TestCommitDiffAcceptanceTwoTurnsAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell CLI fixture is POSIX-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := commitTestRepo(t)
	fakeBin := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakeBin, []byte(`#!/bin/sh
if [ "$1" = "--version" ]; then echo 'fixture 1.0.0'; exit 0; fi
IFS= read -r _
if [ "$FIXTURE_TURN" = 1 ]; then printf 'version one\n' > feature.txt; fi
if [ "$FIXTURE_TURN" = 2 ]; then printf 'version two\n' > feature.txt; fi
if [ "$FIXTURE_TURN" = 4 ]; then printf 'updated draft\n' > file.txt; printf 'new draft\n' > scratch.txt; printf 'ignored draft\n' > ignored-draft.txt; fi
if [ "$FIXTURE_TURN" = 7 ]; then
 printf 'inside change\n' > inside.txt
 printf 'outside change\n' > ../outside.txt
 git config commit.gpgsign true
 git config gpg.program "$FIXTURE_MISSING_GPG"
fi
if [ "$FIXTURE_TURN" = 5 ] || [ "$FIXTURE_TURN" = 6 ]; then
 if [ "$FIXTURE_TURN" = 5 ]; then
  printf 'committed version\n' > file.txt
  git add -- file.txt || exit 2
 else
  printf 'delivered result\n' > delivered.txt
  git add -- delivered.txt || exit 2
 fi
 git -c core.hooksPath=/dev/null -c commit.gpgsign=false -c user.name=Fixture -c user.email=fixture@example.com commit -m 'fixture delivery' >/dev/null || exit 2
 if [ "$FIXTURE_TURN" = 5 ]; then
  printf 'updated draft\n' > file.txt
 else
  printf 'saved draft\n' > draft.txt
 fi
fi
if [ "$FIXTURE_TURN" = 3 ]; then
 printf 'partial result\n' > partial.txt
 git add -- partial.txt
 git -c core.hooksPath=/dev/null -c commit.gpgsign=false -c user.name=Fixture -c user.email=fixture@example.com commit -m 'partial work' >/dev/null || exit 2
fi
printf '%s\n' '{"type":"system","session_id":"session-commit-fixture"}'
if [ "$FIXTURE_TURN" = 3 ]; then
 printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"fixture failure after commit","errors":["fixture failure after commit"]}'
 exit 1
fi
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-commit-fixture","result":"done"}'
`))
	var mu sync.Mutex
	reports := make(map[string][]TaskMessageData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			var body struct {
				Messages []TaskMessageData `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			parts := strings.Split(r.URL.Path, "/")
			taskID := parts[len(parts)-2]
			mu.Lock()
			for _, m := range body.Messages {
				if m.Tool == commitDiffTool {
					reports[taskID] = append(reports[taskID], m)
				}
			}
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{client: NewClient(srv.URL), logger: logger, localPathLocks: NewLocalPathLocker(), workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int), cfg: Config{DaemonID: "commit-fixture", WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second, ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"claude": {Path: fakeBin}}}}
	ref, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeWorktree})
	task := Task{WorkspaceID: "ws-commit", IssueID: "issue-commit", IssueIdentifier: "DEMO-42", RuntimeID: "runtime-commit", AgentID: "agent-commit", AuthToken: "mat_fixture_token", ProjectResources: []ProjectResourceData{{ID: "repo-commit", ResourceType: localDirectoryResourceType, ResourceRef: ref}}}
	type acceptanceRun struct {
		Turn     int               `json:"turn"`
		Status   string            `json:"status"`
		Messages []TaskMessageData `json:"messages"`
	}
	runs := make([]acceptanceRun, 0, 3)
	for i := 1; i <= 3; i++ {
		task.ID = map[int]string{1: "commit-turn-1", 2: "commit-turn-2", 3: "commit-turn-3"}[i]
		task.Agent = &AgentData{ID: task.AgentID, Name: "Fixture", CustomEnv: map[string]string{"HOME": home, "FIXTURE_TURN": string(rune('0' + i))}}
		result, err := d.runTask(context.Background(), task, "claude", 0, logger)
		if i < 3 && (err != nil || result.Status != "completed") {
			t.Fatalf("turn %d: status=%s err=%v", i, result.Status, err)
		}
		if i == 3 && err == nil && result.Status == "completed" {
			t.Fatal("failure fixture was reported as completed")
		}
		mu.Lock()
		messages := append([]TaskMessageData(nil), reports[task.ID]...)
		mu.Unlock()
		if len(messages) != 1 {
			t.Fatalf("turn %d recorded %d ranges; status=%s err=%v", i, len(messages), result.Status, err)
		}
		status := "completed"
		if i == 3 {
			status = "failed"
		}
		runs = append(runs, acceptanceRun{Turn: i, Status: status, Messages: messages})
		task.PriorWorkDir = result.WorkDir
		task.PriorSessionID = result.SessionID
	}
	for i, run := range runs {
		raw, _ := json.Marshal(run.Messages[0].Input)
		expected := []string{"+version one", "-version one", "+partial result"}[i]
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("turn %d missing its change %s", i+1, expected)
		}
		if i == 1 && (!strings.Contains(string(raw), "+version two") || strings.Contains(string(raw), "+version one")) {
			t.Fatal("second turn repeated first turn's patch")
		}
		if i > 0 && run.Messages[0].Input["base"] != runs[i-1].Messages[0].Input["head"] {
			t.Fatalf("turn %d is not based on previous delivery", i+1)
		}
	}
	t.Run("in place without a commit", func(t *testing.T) {
		repo := commitTestRepo(t)
		commitTestWrite(t, repo, "file.txt", "preexisting draft\n")
		commitTestGit(t, repo, "add", "--", "file.txt")
		commitTestWrite(t, repo, "old-draft.txt", "untouched draft\n")
		commitTestWrite(t, repo, ".git/info/exclude", "ignored-draft.txt\n")
		head := commitTestGit(t, repo, "rev-parse", "HEAD")
		index := commitTestGit(t, repo, "diff", "--cached")
		ref, err := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeInPlace})
		if err != nil {
			t.Fatal(err)
		}
		task.ID = "file-changes-in-place"
		task.PriorWorkDir, task.PriorSessionID = "", ""
		task.ProjectResources[0].ResourceRef = ref
		task.Agent.CustomEnv["FIXTURE_TURN"] = "4"
		result, err := d.runTask(context.Background(), task, "claude", 0, logger)
		if err != nil || result.Status != "completed" {
			t.Fatalf("in place: status=%s err=%v", result.Status, err)
		}
		mu.Lock()
		messages := append([]TaskMessageData(nil), reports[task.ID]...)
		mu.Unlock()
		if len(messages) != 1 {
			t.Fatalf("expected one worktree diff, got %d", len(messages))
		}
		input := messages[0].Input
		if input["kind"] != diffWorktree || input["base"] != head || input["head"] != head {
			t.Fatalf("expected worktree diff without a commit, got %v", input)
		}
		raw, err := json.Marshal(input["changes"])
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"-preexisting draft", "+updated draft", "scratch.txt", "+new draft"} {
			if !strings.Contains(string(raw), want) {
				t.Fatalf("missing %q in %s", want, raw)
			}
		}
		for _, excluded := range []string{"old-draft.txt", "ignored-draft.txt", "-original", "CLAUDE.md", ".multica"} {
			if strings.Contains(string(raw), excluded) {
				t.Fatalf("unexpected %q in %s", excluded, raw)
			}
		}
		if commitTestGit(t, repo, "rev-parse", "HEAD") != head || commitTestGit(t, repo, "diff", "--cached") != index {
			t.Fatal("recording changed the repository HEAD or staged files")
		}
		task.ID = "commit-then-restore-file"
		task.Agent.CustomEnv["FIXTURE_TURN"] = "5"
		result, err = d.runTask(context.Background(), task, "claude", 0, logger)
		if err != nil || result.Status != "completed" {
			t.Fatalf("commit then restore: status=%s err=%v", result.Status, err)
		}
		mu.Lock()
		messages = append([]TaskMessageData(nil), reports[task.ID]...)
		mu.Unlock()
		if len(messages) != 2 || messages[0].Input["kind"] != diffCommits || messages[1].Input["kind"] != diffWorktree {
			t.Fatalf("expected both comparisons, got %v", messages)
		}
		commits, _ := json.Marshal(messages[0].Input["changes"])
		if !strings.Contains(string(commits), "+committed version") {
			t.Fatalf("working-tree comparison hid committed content: %s", commits)
		}
		if messages[1].Input["unavailable"] == true || len(messages[1].Input["changes"].([]any)) != 0 {
			t.Fatalf("expected an empty net working-tree diff, got %v", messages[1].Input)
		}
	})
	t.Run("delivered commit and saved draft", func(t *testing.T) {
		repo := commitTestRepo(t)
		ref, err := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeWorktree})
		if err != nil {
			t.Fatal(err)
		}
		task.ID, task.IssueID = "commit-with-saved-draft", "issue-with-draft"
		task.PriorWorkDir, task.PriorSessionID = "", ""
		task.ProjectResources[0].ResourceRef = ref
		task.Agent.CustomEnv["FIXTURE_TURN"] = "6"
		result, err := d.runTask(context.Background(), task, "claude", 0, logger)
		if err != nil || result.Status != "completed" {
			t.Fatalf("worktree delivery: status=%s err=%v", result.Status, err)
		}
		mu.Lock()
		messages := append([]TaskMessageData(nil), reports[task.ID]...)
		mu.Unlock()
		if len(messages) != 2 || messages[0].Input["kind"] != diffCommits || messages[1].Input["kind"] != diffDraft {
			t.Fatalf("expected separate commit and draft, got %v", messages)
		}
		delivered, draft := messages[0].Input, messages[1].Input
		deliveredChanges, _ := json.Marshal(delivered["changes"])
		draftChanges, _ := json.Marshal(draft["changes"])
		if delivered["head"] != draft["base"] || !strings.Contains(string(deliveredChanges), "+delivered result") ||
			strings.Contains(string(deliveredChanges), "draft.txt") || !strings.Contains(string(draftChanges), "+saved draft") ||
			strings.Contains(string(draftChanges), "delivered.txt") {
			t.Fatalf("incorrect delivery or draft range: %s / %s", deliveredChanges, draftChanges)
		}

	})
	t.Run("preserved worktree with a subdirectory resource", func(t *testing.T) {
		repo := commitTestRepo(t)
		subdir := filepath.Join(repo, "sub")
		if err := os.Mkdir(subdir, 0700); err != nil {
			t.Fatal(err)
		}
		commitTestWrite(t, repo, "sub/inside.txt", "inside before\n")
		commitTestWrite(t, repo, "outside.txt", "outside before\n")
		commitTestCommit(t, repo)
		ref, err := json.Marshal(localDirectoryRef{LocalPath: subdir, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeWorktree})
		if err != nil {
			t.Fatal(err)
		}
		task.ID, task.IssueID = "preserved-subdirectory", "issue-subdirectory"
		task.PriorWorkDir, task.PriorSessionID = "", ""
		task.ProjectResources[0].ResourceRef = ref
		task.Agent.CustomEnv["FIXTURE_TURN"] = "7"
		task.Agent.CustomEnv["FIXTURE_MISSING_GPG"] = filepath.Join(t.TempDir(), "missing-gpg")
		result, err := d.runTask(context.Background(), task, "claude", 0, logger)
		if err == nil {
			t.Fatal("expected finalization to fail when signing is unavailable")
		}
		if _, statErr := os.Stat(filepath.Join(result.WorkDir, "inside.txt")); statErr != nil {
			t.Fatalf("worktree was not preserved: %v", statErr)
		}
		mu.Lock()
		messages := append([]TaskMessageData(nil), reports[task.ID]...)
		mu.Unlock()
		if len(messages) != 1 || messages[0].Input["kind"] != diffWorktree || messages[0].Input["unavailable"] != true {
			t.Fatalf("expected an unavailable preserved worktree comparison, got %v", messages)
		}
		if len(messages[0].Input["changes"].([]any)) != 0 {
			t.Fatal("preserved worktree was compared without a starting snapshot")
		}
		for path, expected := range map[string]string{
			filepath.Join(result.WorkDir, "inside.txt"):        "inside change\n",
			filepath.Join(result.WorkDir, "..", "outside.txt"): "outside change\n",
		} {
			content, err := os.ReadFile(path)
			if err != nil || string(content) != expected {
				t.Fatalf("worktree file was not preserved at %s: %v", path, err)
			}
		}
	})
	// Optional local review artifact contains only the public-shaped commit
	// payloads, not prompts, credentials, absolute workdirs or provider logs.
	if output := os.Getenv("MULTICA_COMMIT_ACCEPTANCE_OUTPUT"); output != "" {
		data, err := json.MarshalIndent(map[string]any{"runs": runs}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
