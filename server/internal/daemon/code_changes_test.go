package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func codeChangeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func codeChangeWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newCodeChangeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	codeChangeGit(t, dir, "init", "-q", "-b", "main")
	codeChangeGit(t, dir, "config", "user.name", "Test User")
	codeChangeGit(t, dir, "config", "user.email", "test@test.com")
	codeChangeGit(t, dir, "config", "commit.gpgsign", "false")
	codeChangeGit(t, dir, "config", "gc.auto", "0")
	codeChangeWrite(t, filepath.Join(dir, "app.go"), "package app\n")
	codeChangeGit(t, dir, "add", "-A")
	codeChangeGit(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

// runWorktreeTurn is one issue run in local worktree mode: prepare, let the
// "agent" edit, finalize, and read the run's code change the way runTask does.
func runWorktreeTurn(t *testing.T, repo, taskID string, edit func(workDir string)) []TaskCodeChangeReport {
	t.Helper()
	wt, err := execenv.PrepareLocalWorktree(execenv.LocalWorktreeParams{
		LocalPath:       repo,
		EnvRoot:         t.TempDir(),
		AgentName:       "Lambda",
		TaskID:          taskID,
		ConversationKey: "MUL-7651",
		WorkspaceID:     "11112222-3333-4444-5555-000000000001",
		AgentID:         "11112222-3333-4444-5555-000000000002",
		ConversationID:  "11112222-3333-4444-5555-000000000003",
	}, nil)
	if err != nil {
		t.Fatalf("PrepareLocalWorktree: %v", err)
	}
	edit(wt.WorkDir)
	outcome, err := wt.Finalize(nil)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return worktreeCodeChanges(context.Background(), wt, outcome, nil)
}

func reportByScope(reports []TaskCodeChangeReport, scope string) *TaskCodeChangeReport {
	for i := range reports {
		if reports[i].Scope == scope {
			return &reports[i]
		}
	}
	return nil
}

func reportPaths(r *TaskCodeChangeReport) []string {
	var paths []string
	for _, f := range r.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

// The acceptance case: two runs in a row on one issue in local worktree mode.
// Each run's change holds only that run's work, and the two add up to the
// branch's whole line of work.
func TestWorktreeCodeChangesPerRun(t *testing.T) {
	repo := newCodeChangeRepo(t)

	first := runWorktreeTurn(t, repo, "11112222-3333-4444-5555-aaaaaaaaaaaa", func(dir string) {
		codeChangeWrite(t, filepath.Join(dir, "first.go"), "package app\n\nfunc First() {}\n")
	})
	firstRun := reportByScope(first, "run")
	if firstRun == nil || strings.Join(reportPaths(firstRun), ",") != "first.go" {
		t.Fatalf("first run reports = %+v", first)
	}
	if reportByScope(first, "branch") != nil {
		t.Fatal("the first run is the whole line of work; a branch row would repeat it")
	}
	if firstRun.Source != "local_worktree" || firstRun.Branch != "agent/lambda/mul-7651" || firstRun.Patch == "" {
		t.Fatalf("first run = %+v", firstRun)
	}

	second := runWorktreeTurn(t, repo, "11112222-3333-4444-5555-bbbbbbbbbbbb", func(dir string) {
		codeChangeWrite(t, filepath.Join(dir, "second.go"), "package app\n\nfunc Second() {}\n")
		codeChangeWrite(t, filepath.Join(dir, "app.go"), "package app\n\n// edited\n")
	})
	secondRun := reportByScope(second, "run")
	if secondRun == nil || strings.Join(reportPaths(secondRun), ",") != "app.go,second.go" {
		t.Fatalf("second run must hold only its own files, got %+v", second)
	}
	if strings.Contains(secondRun.Patch, "first.go") {
		t.Fatal("second run's patch carries the first run's work")
	}
	branch := reportByScope(second, "branch")
	if branch == nil {
		t.Fatal("second run must report the branch's whole line of work")
	}
	if got := strings.Join(reportPaths(branch), ","); got != "app.go,first.go,second.go" {
		t.Fatalf("branch files = %s", got)
	}
	if branch.Additions != firstRun.Additions+secondRun.Additions || branch.Deletions != firstRun.Deletions+secondRun.Deletions {
		t.Fatalf("runs +%d-%d and +%d-%d do not add up to the branch's +%d-%d",
			firstRun.Additions, firstRun.Deletions, secondRun.Additions, secondRun.Deletions, branch.Additions, branch.Deletions)
	}
	if secondRun.BaseCommit != firstRun.HeadCommit {
		t.Fatalf("second run starts at %s, want the first run's end %s", secondRun.BaseCommit, firstRun.HeadCommit)
	}
}

// A run that only reads leaves nothing to report.
func TestWorktreeCodeChangesReadOnlyRun(t *testing.T) {
	repo := newCodeChangeRepo(t)
	reports := runWorktreeTurn(t, repo, "11112222-3333-4444-5555-cccccccccccc", func(string) {})
	if len(reports) != 0 {
		t.Fatalf("read-only run reported %+v", reports)
	}
}

// Repository checkout mode: the run's change is the checkout's HEAD when the
// run first saw it against its HEAD at the end, and the branch row measures
// against the remote's default branch.
func TestCheckoutChangeTracker(t *testing.T) {
	upstream := newCodeChangeRepo(t)
	workDir := t.TempDir()
	checkout := filepath.Join(workDir, "app")
	codeChangeGit(t, workDir, "clone", "-q", upstream, checkout)
	codeChangeGit(t, checkout, "config", "user.name", "Test User")
	codeChangeGit(t, checkout, "config", "user.email", "test@test.com")
	codeChangeGit(t, checkout, "checkout", "-q", "-b", "agent/lambda/mul-7651")
	// Work from an earlier run, already on the branch when this run starts.
	codeChangeWrite(t, filepath.Join(checkout, "earlier.go"), "package app\n")
	codeChangeGit(t, checkout, "add", "-A")
	codeChangeGit(t, checkout, "commit", "-q", "-m", "earlier run")

	tracker := newCheckoutChangeTracker(context.Background(), workDir)
	codeChangeWrite(t, filepath.Join(checkout, "now.go"), "package app\n\nfunc Now() {}\n")
	codeChangeGit(t, checkout, "add", "-A")
	codeChangeGit(t, checkout, "commit", "-q", "-m", "this run")
	// Checking the same checkout out again (kept) keeps the start.
	tracker.noteCheckout(context.Background(), checkout, true)

	reports := tracker.collect(context.Background(), nil)
	run := reportByScope(reports, "run")
	if run == nil || strings.Join(reportPaths(run), ",") != "now.go" {
		t.Fatalf("run = %+v", reports)
	}
	if run.Source != "repo_checkout" || run.Branch != "agent/lambda/mul-7651" || run.BaseRef != "origin/main" {
		t.Fatalf("run metadata = %+v", run)
	}
	branch := reportByScope(reports, "branch")
	if branch == nil || strings.Join(reportPaths(branch), ",") != "earlier.go,now.go" {
		t.Fatalf("branch = %+v", branch)
	}

	// A checkout that did not move reports nothing.
	idle := newCheckoutChangeTracker(context.Background(), workDir)
	if got := idle.collect(context.Background(), nil); len(got) != 0 {
		t.Fatalf("unmoved checkout reported %+v", got)
	}
}

// A checkout made during the run — or reset onto a new branch — starts from
// the HEAD it was checked out at.
func TestCheckoutChangeTrackerNewCheckout(t *testing.T) {
	upstream := newCodeChangeRepo(t)
	workDir := t.TempDir()
	tracker := newCheckoutChangeTracker(context.Background(), workDir)

	checkout := filepath.Join(workDir, "app")
	codeChangeGit(t, workDir, "clone", "-q", upstream, checkout)
	codeChangeGit(t, checkout, "config", "user.name", "Test User")
	codeChangeGit(t, checkout, "config", "user.email", "test@test.com")
	tracker.noteCheckout(context.Background(), checkout, false)
	codeChangeWrite(t, filepath.Join(checkout, "new.go"), "package app\n")
	codeChangeGit(t, checkout, "add", "-A")
	codeChangeGit(t, checkout, "commit", "-q", "-m", "work")

	reports := tracker.collect(context.Background(), nil)
	if len(reports) != 1 || reports[0].Scope != "run" || strings.Join(reportPaths(&reports[0]), ",") != "new.go" {
		t.Fatalf("reports = %+v", reports)
	}
}

func TestClientReportTaskCodeChanges(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var body struct {
		Changes []TaskCodeChangeReport `json:"changes"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"stored":1}`))
	}))
	defer srv.Close()

	err := NewClient(srv.URL).ReportTaskCodeChanges(context.Background(), "task-1", []TaskCodeChangeReport{{
		Scope: "run", Source: "local_worktree", RepoKey: "k", RepoLabel: "app",
		BaseCommit: "1111111", HeadCommit: "2222222", FileCount: 1,
	}})
	if err != nil {
		t.Fatalf("ReportTaskCodeChanges: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/api/daemon/tasks/task-1/code-changes" || len(body.Changes) != 1 || body.Changes[0].RepoLabel != "app" {
		t.Fatalf("path %q body %+v", gotPath, body)
	}
}
