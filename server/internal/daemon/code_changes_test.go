package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/codechange"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

type codeChangeReport struct {
	Repos          []codechange.Repo `json:"repos"`
	ReposTruncated bool              `json:"repos_truncated"`
}

// codeChangeTestDaemon is a daemon whose server records every code-change
// report and answers everything else with an empty object.
func codeChangeTestDaemon(t *testing.T, fakeBin string) (*Daemon, func() []codeChangeReport) {
	t.Helper()
	var (
		mu      sync.Mutex
		reports []codeChangeReport
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/code-changes") {
			var body codeChangeReport
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			reports = append(reports, body)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client: NewClient(srv.URL), logger: logger, localPathLocks: NewLocalPathLocker(),
		workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
		cfg: Config{
			DaemonID: "daemon-code-changes", WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second,
			ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"claude": {Path: fakeBin}},
		},
	}
	return d, func() []codeChangeReport {
		mu.Lock()
		defer mu.Unlock()
		return append([]codeChangeReport(nil), reports...)
	}
}

func codeChangeLocalTask(t *testing.T, d *Daemon, repo, mode string) Task {
	t.Helper()
	ref, err := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return Task{
		ID: "task-code-changes-" + mode, WorkspaceID: "ws-cc", IssueID: "issue-cc", IssueIdentifier: "MUL-1",
		RuntimeID: "runtime-cc", AgentID: "agent-cc", AuthToken: "mat_cc_test",
		Agent:            &AgentData{ID: "agent-cc", Name: "cc-agent"},
		ProjectResources: []ProjectResourceData{{ID: "repo-cc", ResourceType: localDirectoryResourceType, ResourceRef: ref}},
	}
}

// A skill makes Prepare write runtime files into the working tree, which must
// never read as the run's changes.
var codeChangeTestSkill = SkillData{ID: "skill-cc", Name: "review-helper", Content: "---\nname: review-helper\ndescription: Review helper\n---\n# Review helper\n"}

// The fake agent edits a tracked file and adds an untracked one, then ends
// the turn successfully.
const codeChangeFakeAgent = `#!/bin/sh
printf 'hello\nworld\n' > README.md
mkdir -p src && printf 'package src\n' > src/new.go
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-code-changes"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-code-changes","result":"done"}'
`

func codeChangeRepo(t *testing.T) string {
	t.Helper()
	repo := createDaemonTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ccGit(t, repo, "add", "README.md")
	ccGit(t, repo, "commit", "-q", "-m", "readme")
	return repo
}

func assertRunChanges(t *testing.T, reports []codeChangeReport) map[string]codechange.File {
	t.Helper()
	if len(reports) != 1 || len(reports[0].Repos) != 1 {
		t.Fatalf("code-change reports = %+v, want one report with one checkout", reports)
	}
	repo := reports[0].Repos[0]
	files := map[string]codechange.File{}
	for _, f := range repo.Files {
		files[f.Path] = f
	}
	if f := files["README.md"]; f.Status != "modified" || f.Additions != 1 {
		t.Errorf("README.md = %+v", f)
	}
	if files["src/new.go"].Status != "added" {
		t.Errorf("untracked src/new.go = %+v", files["src/new.go"])
	}
	for path := range files {
		if strings.HasPrefix(path, ".multica") || strings.HasPrefix(path, ".agent_context") || strings.HasPrefix(path, ".claude") || path == "CLAUDE.md" {
			t.Errorf("runtime file reported as a change: %s", path)
		}
	}
	if !strings.Contains(repo.Patch, "+world") {
		t.Errorf("patch = %q", repo.Patch)
	}
	return files
}

// A real runTask in local-directory worktree mode: the report must be measured
// from the worktree's starting commit and sent before Finalize removes the
// worktree, without Multica's own runtime files.
func TestRunTask_ReportsCodeChangesBeforeWorktreeFinalize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := codeChangeRepo(t)
	fakeBin := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakeBin, []byte(codeChangeFakeAgent))
	d, reports := codeChangeTestDaemon(t, fakeBin)

	task := codeChangeLocalTask(t, d, repo, localDirectoryModeWorktree)
	task.Agent.Skills = []SkillData{codeChangeTestSkill}
	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || result.Status != "completed" {
		t.Fatalf("runTask = %+v, %v", result, err)
	}
	assertRunChanges(t, reports())
}

// In place, the user's own uncommitted work that was there before the run is
// not reported as the run's, and the runtime's files are cleaned up first.
func TestRunTask_InPlaceCodeChangesLeaveTheUsersWorkOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := codeChangeRepo(t)
	// A tracked CLAUDE.md: Prepare adds the runtime brief to it and cleanup
	// takes it out again, so it must not read as a change either way.
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Project notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ccGit(t, repo, "add", "CLAUDE.md")
	ccGit(t, repo, "commit", "-q", "-m", "instructions")
	if err := os.WriteFile(filepath.Join(repo, "user-notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakeBin, []byte(codeChangeFakeAgent))
	d, reports := codeChangeTestDaemon(t, fakeBin)

	task := codeChangeLocalTask(t, d, repo, localDirectoryModeInPlace)
	task.Agent.Skills = []SkillData{codeChangeTestSkill}
	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || result.Status != "completed" {
		t.Fatalf("runTask = %+v, %v", result, err)
	}
	files := assertRunChanges(t, reports())
	if _, ok := files["user-notes.txt"]; ok {
		t.Error("the user's pre-existing untracked file was reported as the run's change")
	}
}

// A checkout the agent creates mid-run through `multica repo checkout` is
// measured from the commit it was checked out at, so the agent's own commits
// on it are reported even when its branch tracks nothing.
func TestRepoCheckoutRecordsCodeChangeBaseline(t *testing.T) {
	const workspaceID = "ws-checkout"
	const repoURL = "https://github.com/org/repo.git"
	workDir := t.TempDir()
	checkout := codeChangeRepo(t)
	base := strings.TrimSpace(ccGitOutput(t, checkout, "rev-parse", "HEAD"))
	cache := &fixedPathRepoCache{recordingRepoCache: recordingRepoCache{lookupPath: "/cache/org/repo.git"}, path: checkout}
	d := newRepoCheckoutTestDaemon(t, workspaceID, repoURL, workDir, cache)
	baselines := codechange.NewBaselines()
	d.repoCheckoutTasksMu.Lock()
	active := d.repoCheckoutTasks["mat_repo_checkout_test"]
	active.CodeChanges = baselines
	d.repoCheckoutTasks["mat_repo_checkout_test"] = active
	d.repoCheckoutTasksMu.Unlock()

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"url":"` + repoURL + `","workspace_id":"` + workspaceID + `","workdir":"` + workDir + `","task_id":"task-1"}`)
	d.repoCheckoutHandler().ServeHTTP(rec, authorizedRepoCheckoutRequest(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("checkout = %d: %s", rec.Code, rec.Body.String())
	}

	if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("hello\nagent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ccGit(t, checkout, "commit", "-q", "-am", "agent commit")
	got := codechange.Snapshot(context.Background(), checkout, baselines)
	if len(got.Repos) != 1 || got.Repos[0].Base != base || len(got.Repos[0].Files) != 1 {
		t.Fatalf("snapshot = %+v, want the agent's commit measured from %s", got.Repos, base)
	}
}

type fixedPathRepoCache struct {
	recordingRepoCache
	path string
}

func (c *fixedPathRepoCache) CreateWorktree(params repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	if _, err := c.recordingRepoCache.CreateWorktree(params); err != nil {
		return nil, err
	}
	return &repocache.WorktreeResult{Path: c.path, BranchName: "agent/test"}, nil
}

func ccGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	ccGitOutput(t, dir, args...)
}

func ccGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
