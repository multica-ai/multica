package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestCommitDiffCheckoutBaseline(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		t.Run(map[bool]string{false: "linked", true: "isolated"}[isolated], func(t *testing.T) {
			source := commitTestRepo(t)
			cache := repocache.New(t.TempDir(), slog.Default())
			const workspace = "ws-diff-checkout"
			if err := cache.Sync(workspace, []repocache.RepoInfo{{URL: source}}); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			workDir := filepath.Join(root, "workdir")
			if err := os.Mkdir(workDir, 0700); err != nil {
				t.Fatal(err)
			}
			initial, err := cache.CreateWorktree(repocache.WorktreeParams{
				WorkspaceID: workspace, RepoURL: source, WorkDir: workDir,
				AgentName: "Test Agent", TaskID: "previous-task", IsolatedGitMetadata: isolated,
			})
			if err != nil {
				t.Fatal(err)
			}
			first := newCommitDiffRecorder()
			first.restoreCheckouts(root, workDir)
			first.record(initial.Path)
			first.close()
			recorder := newCommitDiffRecorder()
			t.Cleanup(recorder.close)
			recorder.restoreCheckouts(root, workDir)
			oldHead := commitTestGit(t, initial.Path, "rev-parse", "HEAD")
			commitTestWrite(t, source, "upstream.txt", "upstream change\n")
			newHead := commitTestCommit(t, source)
			if err := cache.Sync(workspace, []repocache.RepoInfo{{URL: source}}); err != nil {
				t.Fatal(err)
			}
			d := newRepoCheckoutTestDaemon(t, workspace, source, workDir, cache)
			d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{
				WorkspaceID: workspace, TaskID: "task-1", AgentID: "agent-1",
				AgentName: "Test Agent", WorkDir: workDir, CommitDiff: recorder,
			})
			checkout := func() repocache.WorktreeResult {
				t.Helper()
				mode := ""
				if isolated {
					mode = repoCheckoutModeIsolated
				}
				body, err := json.Marshal(repoCheckoutRequest{
					URL: source, WorkspaceID: workspace, TaskID: "task-1",
					WorkDir: workDir, CheckoutMode: mode,
				})
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				d.repoCheckoutHandler().ServeHTTP(response, authorizedRepoCheckoutRequest(strings.NewReader(string(body))))
				if response.Code != http.StatusOK {
					t.Fatalf("checkout: status=%d body=%s", response.Code, response.Body.String())
				}
				var result repocache.WorktreeResult
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			collect := func() []map[string]any {
				t.Helper()
				var repos []commitRepo
				for _, repo := range recorder.repos {
					repos = append(repos, repo)
				}
				return collectRunDiffs(context.Background(), repos, true)
			}
			switched := checkout()
			if switched.Kept != "" || oldHead == newHead || commitTestGit(t, switched.Path, "rev-parse", "HEAD") != newHead {
				t.Fatal("fixture did not switch a clean checkout to the updated upstream")
			}
			diffs := collect()
			if len(diffs) != 1 || diffs[0]["kind"] != diffWorktree || diffs[0]["unavailable"] == true ||
				len(diffs[0]["changes"].([]map[string]any)) != 0 {
				t.Fatalf("checkout-only upstream changes were attributed to this run: %v", diffs)
			}
			commitTestWrite(t, switched.Path, "agent.txt", "agent commit\n")
			commitTestGit(t, switched.Path, "add", "-A")
			commitTestGit(t, switched.Path, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "agent work")
			commitTestWrite(t, switched.Path, "draft.txt", "agent draft\n")
			kept := checkout()
			if kept.Kept != repocache.KeptTaskBranch {
				t.Fatalf("expected same-task checkout to be kept, got %q", kept.Kept)
			}
			diffs = collect()
			if len(diffs) != 2 || diffs[0]["base"] != newHead {
				t.Fatalf("repeated checkout lost the commit baseline: %v", diffs)
			}
			raw, _ := json.Marshal(diffs)
			if !strings.Contains(string(raw), "+agent commit") || !strings.Contains(string(raw), "+agent draft") ||
				strings.Contains(string(raw), "upstream.txt") {
				t.Fatal("kept checkout lost agent work or included upstream work")
			}
			// A different task must also preserve the baseline when local work is kept.
			d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{
				WorkspaceID: workspace, TaskID: "task-1", AgentName: "Other Agent",
				WorkDir: workDir, CommitDiff: recorder,
			})
			if kept = checkout(); kept.Kept != repocache.KeptLocalWork {
				t.Fatalf("expected local work to be kept, got %q", kept.Kept)
			}
			after, _ := json.Marshal(collect())
			if string(after) != string(raw) {
				t.Fatal("kept local work changed the run comparisons")
			}
		})
	}
}
