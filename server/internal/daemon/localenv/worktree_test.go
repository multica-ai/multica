//go:build !windows

package localenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

type worktreeFixture struct {
	t        *testing.T
	repo     string
	root     string
	wt       *execenv.LocalWorktree
	m        *Manager
	managers []*Manager
	task     Task
	params   execenv.LocalWorktreeParams
}

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	b, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, b, err)
	}
	return strings.TrimSpace(string(b))
}

func fixtureWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func newWorktreeFixture(t *testing.T, ttl string) *worktreeFixture {
	t.Helper()
	repo := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	fixtureGit(t, repo, "init", "--template="+t.TempDir(), "-b", "main")
	// These commits are synthetic fixture data in a fresh repository.
	for k, v := range map[string]string{"user.name": "Test User", "user.email": "test@example.invalid", "gc.auto": "0", "maintenance.auto": "false"} {
		fixtureGit(t, repo, "config", k, v)
	}
	fixtureWrite(t, filepath.Join(repo, "index.html"), "served checkout payload\n")
	fixtureWrite(t, filepath.Join(repo, ".gitignore"), ".dependencies/\n")
	config := ""
	for _, name := range []string{"web", "api"} {
		config += fmt.Sprintf("[environments.%s]\nstart = %q\nurl = \"http://127.0.0.1:$MULTICA_PORT\"\nttl = %q\n", name, helperCommand("serve-files"), ttl)
	}
	fixtureWrite(t, filepath.Join(repo, ConfigPath), config)
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "initial fixture")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	params := execenv.LocalWorktreeParams{LocalPath: repo, EnvRoot: root, AgentName: "test", TaskID: "11112222-3333-4444-5555-666677778888", ConversationKey: "test-issue", WorkspaceID: "test-workspace", AgentID: "test-agent", ConversationID: "test-issue"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wt, err := execenv.PrepareLocalWorktree(params, logger)
	if err != nil {
		t.Fatal(err)
	}
	f := &worktreeFixture{t: t, repo: repo, root: root, wt: wt, task: Task{TaskID: params.TaskID, IssueID: params.ConversationID, WorkspaceID: params.WorkspaceID, RootDir: root}, params: params}
	// Register collectors after every TempDir, before any Manager can Load or
	// Start work. Each owned Manager gets a separate stop callback; those run
	// first, including when another cleanup callback calls Fatal.
	dir := t.TempDir()
	t.Cleanup(func() { f.joinWorktreeReleases(true) })
	f.useManager(NewManager(dir, logger))
	// Stand in for dependencies installed inside the actual checkout.
	fixtureWrite(t, filepath.Join(wt.Path, ".dependencies/package/file"), "installed dependency\n")
	return f
}

// Keep the original Manager alive across recovery and shared-manager tests.
// Ownership is registered before Load, which can itself start release work.
func (f *worktreeFixture) useManager(m *Manager) {
	for _, owned := range f.managers {
		if owned == m {
			f.m = m
			return
		}
	}
	f.managers = append(f.managers, m)
	f.m = m
	f.t.Cleanup(func() {
		for _, env := range m.List(f.task) {
			if !f.ownsDirectory(env.Dir) || (env.State != StateRunning && env.State != StateStarting) {
				continue
			}
			// Separate callbacks also collect the remaining environments if one
			// process audit or Stop fails with Fatal.
			f.t.Cleanup(func() {
				f.auditProcess(env)
				if _, err := m.StopID(strings.TrimPrefix(env.Key, "config:")); err != nil {
					f.t.Error(err)
				}
			})
		}
	})
}

func (f *worktreeFixture) ownsDirectory(dir string) bool {
	return dir == f.wt.Path || dir == f.repo
}

// Final cleanup joins every owned leader. Mid-test collection selects only
// exited groups, so a partial Stop never waits for a shared API
// that is still serving. Group exit selects the waiter to join; it is not the
// completion signal. The original leaderDone and a subsequent lock acquisition
// cover wait's endLocked/persist/release work, even after f.m was replaced.
func (f *worktreeFixture) collectWorktreeReleases() {
	f.joinWorktreeReleases(false)
}

func (f *worktreeFixture) joinWorktreeReleases(all bool) {
	f.t.Helper()
	for _, m := range f.managers {
		m.mu.Lock()
		var leaders []<-chan struct{}
		for _, e := range m.envs {
			if f.ownsDirectory(e.rec.RepoRoot) && e.leaderDone != nil && (all || !groupAlive(e.rec.PID)) {
				leaders = append(leaders, e.leaderDone)
			}
		}
		m.mu.Unlock()
		waitWorktrees(leaders)
		// leaderDone closes under mu before wait finishes its record writes.
		m.mu.Lock()
		m.mu.Unlock()
	}
	// Every original waiter above has now reserved its release, if any.
	// Take each Manager's real operation notifications under its mutex and
	// wait outside it. Do not Tick, retry or manufacture a release here.
	for _, m := range f.managers {
		m.mu.Lock()
		pending := m.cleanupDoneLocked(checkoutPath(f.wt.Path))
		m.mu.Unlock()
		waitWorktrees(pending)
	}
}

func (f *worktreeFixture) start(name string) Environment {
	f.t.Helper()
	e, err := f.m.Start(f.task, f.wt.Path, name, os.Getenv("PATH"))
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e = f.m.WaitReady(ctx, e.Key)
	if e.State != StateRunning {
		f.t.Fatalf("environment not ready: %+v", e)
	}
	return e
}

// Check the exact test child before letting the production lifecycle signal it.
// Inside a Multica task, also compare it with the actual workspace daemon.
func (f *worktreeFixture) auditProcess(e Environment) {
	f.t.Helper()
	pid := e.pgid
	if !groupAlive(pid) {
		return
	}
	pg, err := syscall.Getpgid(pid)
	if err != nil || pg != pid {
		f.t.Fatalf("not a test-owned leader: pid=%d pgid=%d err=%v", pid, pg, err)
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,pgid=,command=").CombinedOutput()
	if err != nil {
		f.t.Fatal(err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 || fields[1] != strconv.Itoa(os.Getpid()) || !strings.Contains(string(out), "TestHelperProcess") {
		f.t.Fatalf("unexpected test process: %q", out)
	}
	if cli := os.Getenv("MULTICA_CLI_PATH"); cli != "" {
		raw, err := exec.Command(cli, "daemon", "status", "--output", "json").Output()
		if err != nil {
			f.t.Fatal(err)
		}
		var d struct {
			PID int `json:"pid"`
		}
		if err = json.Unmarshal(raw, &d); err != nil || d.PID <= 0 || d.PID == pid {
			f.t.Fatalf("invalid daemon comparison: %v daemon=%d own=%d", err, d.PID, pid)
		}
		f.t.Logf("PRESTOP daemon=%d owned=%s checkout=%s", d.PID, strings.TrimSpace(string(out)), f.wt.Path)
	}
}

func (f *worktreeFixture) stop(name string) {
	f.t.Helper()
	var env Environment
	for _, e := range f.m.List(f.task) {
		if e.Name == name {
			env = e
		}
	}
	f.auditProcess(env)
	stopped, err := f.m.Stop(f.task, name)
	if err != nil {
		f.t.Fatal(err)
	}
	if stopped.State != StateStopped || groupAlive(env.pgid) {
		f.t.Fatalf("Stop did not collect group %d: %+v", env.pgid, stopped)
	}
	f.collectWorktreeReleases()
	f.t.Logf("STOP owned=%d state=%s", env.pgid, stopped.State)
}

func (f *worktreeFixture) present(e Environment) {
	f.t.Helper()
	for _, name := range []string{"index.html", ".git", ".dependencies/package/file"} {
		if _, err := os.Stat(filepath.Join(f.wt.Path, name)); err != nil {
			f.t.Fatalf("live checkout missing %s: %v", name, err)
		}
	}
	if !strings.Contains(fixtureGit(f.t, f.repo, "worktree", "list", "--porcelain"), f.wt.Path) {
		f.t.Fatal("live worktree is not registered")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(e.URL + "/index.html")
	if err != nil {
		f.t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || string(b) != "served checkout payload\n" {
		f.t.Fatalf("checkout HTTP: status=%d body=%q err=%v", resp.StatusCode, b, err)
	}
	f.t.Logf("CHECKOUT path=%s file=present dependencies=present registered=true HTTP=200 body=%q", f.wt.Path, b)
}

func (f *worktreeFixture) removed() {
	f.t.Helper()
	if _, err := os.Lstat(f.wt.Path); !errors.Is(err, os.ErrNotExist) {
		f.t.Fatalf("checkout not released: %v", err)
	}
	if strings.Contains(fixtureGit(f.t, f.repo, "worktree", "list", "--porcelain"), f.wt.Path) {
		f.t.Fatal("released checkout still registered")
	}
	if b, err := os.ReadFile(filepath.Join(f.repo, "index.html")); err != nil || string(b) != "served checkout payload\n" {
		f.t.Fatalf("source checkout changed: %q %v", b, err)
	}
	f.t.Logf("RELEASED path=%s file=absent registration=false source=untouched", f.wt.Path)
}

func TestDeclaredEnvironmentSurvivesWorktreeFinalize(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	e := f.start("web")
	f.present(e)
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil {
		t.Fatal(err)
	}
	if !out.AutoCommitted || out.RetainedWorktree == nil || out.PreservedPath != "" {
		t.Fatalf("finalization: %+v", out)
	}
	tip := fixtureGit(t, f.repo, "rev-parse", out.Branch)
	if got := fixtureGit(t, f.repo, "show", out.Branch+":delivered.txt"); got != "task delivery" {
		t.Fatalf("branch lost delivery: %q", got)
	}
	f.present(e)
	f.stop("web")
	f.removed()
	if got := fixtureGit(t, f.repo, "rev-parse", out.Branch); got != tip {
		t.Fatalf("release changed delivered branch: %s != %s", got, tip)
	}
}

func TestDeclaredEnvironmentsShareFinalizedWorktree(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("secondStartsAfterFinalize=%v", late), func(t *testing.T) {
			f := newWorktreeFixture(t, "24h")
			fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
			f.start("web")
			var api Environment
			if !late {
				api = f.start("api")
			}
			out, err := f.m.FinalizeWorktree(f.wt)
			if err != nil || out.RetainedWorktree == nil {
				t.Fatalf("Finalize: %+v %v", out, err)
			}
			if late {
				api = f.start("api")
			}
			f.stop("web")
			f.present(api)
			// Restore from actual persisted records, without starting a daemon.
			restored := NewManager(f.m.dir, f.m.logger)
			f.useManager(restored)
			if err := restored.Load(); err != nil {
				t.Fatal(err)
			}
			f.present(api)
			f.stop("api")
			f.removed()
		})
	}
}

func TestDeclaredEnvironmentReleasesWorktreeAtRealTTL(t *testing.T) {
	f := newWorktreeFixture(t, "1m")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	e := f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	f.present(e)
	if e.ExpiresAt == nil {
		t.Fatal("no real TTL")
	}
	// The configured minimum TTL elapses on the real clock; Tick and Stop use
	// their production timing, without overriding now or shortening grace periods.
	t.Logf("waiting for real TTL: started=%s expires=%s", e.StartedAt, e.ExpiresAt)
	for time.Now().Before(*e.ExpiresAt) {
		time.Sleep(250 * time.Millisecond)
	}
	f.auditProcess(e)
	f.m.Tick()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		list := f.m.List(f.task)
		if len(list) == 1 && list[0].State == StateStopped && !groupAlive(e.pgid) {
			if list[0].Detail != "expired" {
				t.Fatalf("TTL detail=%q", list[0].Detail)
			}
			// Stop records process exit before the lock-free cleanup finishes.
			// Collect the real filesystem/registration completion separately.
			f.m.Tick()
			f.eventuallyRemoved()
			t.Logf("TTL STOP owned=%d state=%s detail=%s", e.pgid, list[0].State, list[0].Detail)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("real TTL failed to stop and release the environment")
}

func TestWorktreeFinalizeWithoutDeclaredEnvironment(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil || out.RetainedWorktree != nil {
		t.Fatalf("Finalize: %+v %v", out, err)
	}
	f.removed()
	if got := fixtureGit(t, f.repo, "show", out.Branch+":delivered.txt"); got != "task delivery" {
		t.Fatalf("delivery=%q", got)
	}
}

func TestWorktreeFinalizeIgnoresUnrelatedEnvironment(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	other := newWorktreeFixture(t, "24h")
	// One manager supervises two independent checkouts.
	other.useManager(f.m)
	e := other.start("web")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil || out.RetainedWorktree != nil {
		t.Fatalf("Finalize: %+v %v", out, err)
	}
	f.removed()
	other.present(e)
	other.stop("web")
}

func TestFailedWorktreeDeliveryIsNotReleasedByEnvironmentStop(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "recoverable task work\n")
	f.start("web")
	f.wt.AbortWithReason(errors.New("task sidecar cleanup failed"))
	out, err := f.m.FinalizeWorktree(f.wt)
	if err == nil || out.PreservedPath != f.wt.Path || out.RetainedWorktree != nil {
		t.Fatalf("Finalize should preserve failed delivery: %+v %v", out, err)
	}
	f.stop("web")
	if got, err := os.ReadFile(filepath.Join(f.wt.Path, "delivered.txt")); err != nil || string(got) != "recoverable task work\n" {
		t.Fatalf("failed delivery lost work: %q %v", got, err)
	}
	if !strings.Contains(fixtureGit(t, f.repo, "worktree", "list", "--porcelain"), f.wt.Path) {
		t.Fatal("failed delivery lost registration")
	}
}

func TestEnvironmentReleasePreservesAgentCommitsLeftoversAndContinuation(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent choice\n")
	fixtureGit(t, f.wt.Path, "add", "delivered.txt")
	fixtureGit(t, f.wt.Path, "commit", "-m", "agent fixture commit")
	chosen := fixtureGit(t, f.wt.Path, "rev-parse", "HEAD")
	fixtureWrite(t, filepath.Join(f.wt.Path, "leftover.txt"), "saved off branch\n")
	f.start("web")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil {
		t.Fatal(err)
	}
	if out.AutoCommitted || out.LeftoverRef == "" {
		t.Fatalf("leftovers should stay off branch: %+v", out)
	}
	if got := fixtureGit(t, f.repo, "rev-parse", out.Branch); got != chosen {
		t.Fatalf("agent commit changed %s", got)
	}
	if got := fixtureGit(t, f.repo, "show", out.LeftoverRef+":leftover.txt"); got != "saved off branch" {
		t.Fatalf("leftovers=%q", got)
	}
	state := fixtureGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/multica/local-state/")
	f.stop("web")
	f.removed()
	if got := fixtureGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/multica/local-state/"); got != state {
		t.Fatalf("release changed branch record: %s", got)
	}
	if got := fixtureGit(t, f.repo, "show", out.LeftoverRef+":leftover.txt"); got != "saved off branch" {
		t.Fatalf("release lost leftovers=%q", got)
	}
	params := f.params
	params.EnvRoot = t.TempDir()
	params.TaskID = "11112222-3333-4444-5555-999900001111"
	next, err := execenv.PrepareLocalWorktree(params, f.m.logger)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Continued || next.Branch != out.Branch {
		t.Fatalf("conversation not continued: %+v", next)
	}
	if _, err := os.Stat(filepath.Join(next.Path, "leftover.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leftovers leaked onto next branch: %v", err)
	}
	if _, err := f.m.FinalizeWorktree(next); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyEnvironmentWorktreeDropsOnlyItsEmptyBranch(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	f.start("web")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil || out.Branch != "" || out.RetainedWorktree == nil {
		t.Fatalf("empty delivery: %+v %v", out, err)
	}
	f.stop("web")
	f.removed()
	if b, err := exec.Command("git", "-C", f.repo, "show-ref", "--verify", "refs/heads/"+f.wt.Branch).CombinedOutput(); err == nil {
		t.Fatalf("empty branch survived: %s", b)
	}
	fixtureGit(t, f.repo, "rev-parse", "main")
}

// A logger observes the actual cross-process Git lock wait; it does not replace
// the lock, finalization, process creation, or filesystem cleanup under test.
type worktreeLockObserver struct{ entered chan struct{} }

func (h worktreeLockObserver) Enabled(context.Context, slog.Level) bool { return true }
func (h worktreeLockObserver) Handle(_ context.Context, r slog.Record) error {
	if strings.Contains(r.Message, "waiting for another task's git section") {
		select {
		case h.entered <- struct{}{}:
		default:
		}
	}
	return nil
}
func (h worktreeLockObserver) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h worktreeLockObserver) WithGroup(string) slog.Handler      { return h }

func TestFailedEnvironmentStartConcurrentWithWorktreeCompletion(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	unlock := holdWorktreeGit(t, f)
	entered := make(chan struct{}, 1)
	f.m.logger = slog.New(worktreeLockObserver{entered: entered})
	var out execenv.LocalWorktreeOutcome
	join := startLockedWorktree(t, unlock, func() error {
		var err error
		out, err = f.m.FinalizeWorktree(f.wt)
		return err
	})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		err := join()
		t.Fatalf("production finalizer never reached lock barrier: %+v %v", out, err)
	}
	// The production finalizer is waiting at the real Git lock. A concurrent
	// Start must not publish a lease on the checkout being disposed.
	startDone := make(chan error, 1)
	go func() { _, err := f.m.Start(f.task, f.wt.Path, "web", os.Getenv("PATH")); startDone <- err }()
	err := join()
	startErr := <-startDone
	if err != nil || out.RetainedWorktree != nil {
		t.Fatalf("completion: %+v %v", out, err)
	}
	if startErr == nil {
		t.Fatal("Start succeeded after completion removed its checkout")
	}
	if len(f.m.List(f.task)) != 0 {
		t.Fatal("failed start left an environment record")
	}
	f.removed()
	t.Logf("ordered failed Start: %v", startErr)
}

func TestSharedEnvironmentClockTTLsReleaseOnlyTheLastCheckoutUser(t *testing.T) {
	f := newWorktreeFixture(t, "1m")
	advance := fixtureClock(f)
	data, err := os.ReadFile(filepath.Join(f.wt.Path, ConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	// Advance past the first expiry while remaining strictly before the second.
	config := strings.SplitN(string(data), "[environments.api]", 2)
	if len(config) != 2 {
		t.Fatal("fixture has no API declaration")
	}
	fixtureWrite(t, filepath.Join(f.wt.Path, ConfigPath), config[0]+"[environments.api]"+strings.Replace(config[1], `ttl = "1m"`, `ttl = "2m"`, 1))
	web := f.start("web")
	api := f.start("api")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	expire := func(e Environment) {
		t.Helper()
		if e.ExpiresAt == nil {
			t.Fatal("missing real expiry")
		}
		t.Logf("advancing past %s TTL: %s", e.Name, e.ExpiresAt)
		advance(e.ExpiresAt.Add(time.Nanosecond))
		f.auditProcess(e)
		f.m.Tick()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			for _, state := range f.m.List(f.task) {
				if state.Key == e.Key && state.State == StateStopped && !groupAlive(e.pgid) {
					if state.Detail != "expired" {
						t.Fatalf("expiry detail=%q", state.Detail)
					}
					t.Logf("TTL STOP owned=%d name=%s detail=%s", e.pgid, e.Name, state.Detail)
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s real TTL did not finish stopping", e.Name)
	}
	if web.ExpiresAt == nil || api.ExpiresAt == nil || !web.ExpiresAt.Before(*api.ExpiresAt) {
		t.Fatal("shared TTLs are not ordered")
	}
	expire(web)
	if !f.m.now().Before(*api.ExpiresAt) {
		t.Fatal("first clock advance crossed API expiry")
	}
	f.present(api)
	expire(api)
	f.eventuallyRemoved()
}

func TestRestartKeepsTheFinalizedEnvironmentCheckout(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	old := f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	f.auditProcess(old)
	id, ok := IDFromKey(old.Key)
	if !ok {
		t.Fatal("invalid environment key")
	}
	next, err := f.m.Restart(id, func(string) (Task, bool) { return f.task, true })
	if err != nil {
		t.Fatalf("Restart discarded the checkout before starting its replacement: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	next = f.m.WaitReady(ctx, next.Key)
	if next.State != StateRunning || next.Key == old.Key || groupAlive(old.pgid) {
		t.Fatalf("restart not collected/replaced: %+v", next)
	}
	f.present(next)
	f.stop("web")
	f.removed()
}

func TestFailedRestartReleasesItsTemporaryCheckoutPin(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "task delivery\n")
	old := f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	f.auditProcess(old)
	id, ok := IDFromKey(old.Key)
	if !ok {
		t.Fatal("invalid environment key")
	}
	_, err := f.m.Restart(id, func(string) (Task, bool) { task := f.task; task.IssueID = "another-issue"; return task, true })
	if err == nil || !strings.Contains(err.Error(), "another issue") {
		t.Fatalf("expected original ownership rejection: %v", err)
	}
	if groupAlive(old.pgid) {
		t.Fatal("failed restart left old process alive")
	}
	f.removed()
}

func TestEnvironmentStopKeepsTheUserOwnedSourceDirectory(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	task := f.task
	task.RootDir = f.repo
	env, err := f.m.Start(task, f.repo, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	env = f.m.WaitReady(ctx, env.Key)
	if env.State != StateRunning {
		t.Fatalf("source environment not ready: %+v", env)
	}
	f.stop("web")
	if b, err := os.ReadFile(filepath.Join(f.repo, "index.html")); err != nil || string(b) != "served checkout payload\n" {
		t.Fatalf("user source removed: %q %v", b, err)
	}
	fixtureGit(t, f.repo, "rev-parse", "main")
}

// worktreeEvents observes supported production log messages, leaving Git,
// finalization, process startup and cleanup unchanged.
type worktreeEvents struct {
	mu       sync.Mutex
	waits    chan struct{}
	warnings int
}

func (w *worktreeEvents) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.Contains(string(p), "waiting for another task's git section") {
		select {
		case w.waits <- struct{}{}:
		default:
		}
	}
	if strings.Contains(string(p), "release environment worktree failed") {
		w.warnings++
	}
	return len(p), nil
}
func (w *worktreeEvents) warningCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.warnings
}

func fixtureClock(f *worktreeFixture) func(time.Time) {
	var nanos atomic.Int64
	nanos.Store(time.Now().UnixNano())
	f.m.now = func() time.Time { return time.Unix(0, nanos.Load()) }
	return func(now time.Time) { nanos.Store(now.UnixNano()) }
}

func holdWorktreeGit(t *testing.T, f *worktreeFixture) func() {
	t.Helper()
	lock, err := os.OpenFile(filepath.Join(f.repo, ".git", "multica-worktree.lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
				t.Error(err)
			}
			lock.Close()
		})
	}
	t.Cleanup(unlock)
	return unlock
}

// Own the unlock and the real producer completion before starting it. Cleanup
// runs even when the test body calls Fatal/Goexit, before Manager collection and
// TempDir removal. Normal assertions share the result without receiving twice.
func startLockedWorktree(t *testing.T, unlock func(), run func() error) func() error {
	t.Helper()
	done := make(chan error, 1)
	join := sync.OnceValue(func() error {
		unlock()
		return <-done
	})
	t.Cleanup(func() {
		if err := join(); err != nil {
			t.Errorf("worktree operation: %v", err)
		}
	})
	go func() { done <- run() }()
	return join
}

func TestWorktreeGitWaitDoesNotBlockOtherRepositories(t *testing.T) {
	for _, mode := range []string{"finalize-empty", "finalize-live", "release"} {
		t.Run(mode, func(t *testing.T) {
			a := newWorktreeFixture(t, "24h")
			b := newWorktreeFixture(t, "1m")
			b.useManager(a.m)
			advance := fixtureClock(b) // Installed before any probes exist.
			fixtureWrite(t, filepath.Join(a.wt.Path, "delivered.txt"), "task delivery\n")
			events := &worktreeEvents{waits: make(chan struct{}, 10)}
			a.m.logger = slog.New(slog.NewTextHandler(events, nil))
			var env Environment
			if mode != "finalize-empty" {
				env = a.start("web")
			}
			if mode == "release" {
				if _, err := a.m.FinalizeWorktree(a.wt); err != nil {
					t.Fatal(err)
				}
				a.auditProcess(env)
			}
			unlock := holdWorktreeGit(t, a)
			join := startLockedWorktree(t, unlock, func() error {
				if mode == "release" {
					_, err := a.m.Stop(a.task, "web")
					return err
				}
				_, err := a.m.FinalizeWorktree(a.wt)
				return err
			})
			select {
			case <-events.waits:
			case <-time.After(10 * time.Second):
				join()
				t.Fatal("real Git lock wait was not observed")
			}
			// Completion is required WHILE the real lock is still held.
			otherDone := make(chan error, 1)
			go func() {
				b.m.Snapshot()
				e, err := b.m.Start(b.task, b.wt.Path, "web", os.Getenv("PATH"))
				if err != nil {
					otherDone <- err
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				e = b.m.WaitReady(ctx, e.Key)
				cancel()
				if e.State != StateRunning {
					otherDone <- fmt.Errorf("B not ready: %+v", e)
					return
				}
				b.auditProcess(e)
				if _, err := b.m.Stop(b.task, "web"); err != nil {
					otherDone <- err
					return
				}
				api, err := b.m.Start(b.task, b.wt.Path, "api", os.Getenv("PATH"))
				if err != nil {
					otherDone <- err
					return
				}
				b.auditProcess(api)
				advance(api.ExpiresAt.Add(time.Nanosecond))
				b.m.Tick()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					for _, state := range b.m.List(b.task) {
						if state.Key == api.Key && state.State == StateStopped && !groupAlive(api.pgid) {
							otherDone <- nil
							return
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				otherDone <- fmt.Errorf("B TTL did not finish while A was blocked")
			}()
			var otherErr error
			blocked := false
			select {
			case otherErr = <-otherDone:
			case <-time.After(5 * time.Second):
				blocked = true
			}
			if err := join(); err != nil {
				t.Error(err)
			}
			if blocked {
				otherErr = <-otherDone
				t.Error("unrelated Snapshot/Start/Stop/Tick blocked behind repo A Git lock")
			}
			if otherErr != nil {
				t.Error(otherErr)
			}
			advance(time.Now())
		})
	}
}

func (f *worktreeFixture) eventuallyRemoved() {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(f.wt.Path); os.IsNotExist(err) && !f.m.RetainsWorktree(f.wt.Path) {
			f.removed()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("checkout cleanup did not complete")
}

func TestWorktreeReservationExcludesStartsAndRestart(t *testing.T) {
	for _, mode := range []string{"finalize-empty", "finalize-live", "release"} {
		t.Run(mode, func(t *testing.T) {
			f := newWorktreeFixture(t, "24h")
			fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "delivery\n")
			nested := filepath.Join(f.wt.Path, ".dependencies", "nested")
			if err := os.MkdirAll(nested, 0700); err != nil {
				t.Fatal(err)
			}
			var e Environment
			if mode != "finalize-empty" {
				e = f.start("web")
			}
			if mode == "release" {
				if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
					t.Fatal(err)
				}
				f.auditProcess(e)
			}
			events := &worktreeEvents{waits: make(chan struct{}, 10)}
			f.m.logger = slog.New(slog.NewTextHandler(events, nil))
			unlock := holdWorktreeGit(t, f)
			join := startLockedWorktree(t, unlock, func() error {
				if mode == "release" {
					_, err := f.m.Stop(f.task, "web")
					return err
				}
				_, err := f.m.FinalizeWorktree(f.wt)
				return err
			})
			select {
			case <-events.waits:
			case <-time.After(10 * time.Second):
				join()
				t.Fatal("real lock barrier not reached")
			}
			if !f.m.RetainsWorktree(f.root) {
				t.Error("in-flight checkout is not protected from GC")
			}
			for _, dir := range []string{f.wt.Path, nested} {
				if got, err := f.m.Start(f.task, dir, "api", os.Getenv("PATH")); err == nil {
					t.Errorf("Start crossed the deletion reservation: %+v", got)
				}
			}
			if mode == "release" {
				id, _ := IDFromKey(e.Key)
				if _, err := f.m.Restart(id, func(string) (Task, bool) { return f.task, true }); err == nil {
					t.Error("Restart pinned an already releasing checkout")
				}
			}
			if len(f.m.List(f.task)) > 1 {
				t.Error("rejected starts published a record")
			}
			if err := join(); err != nil {
				t.Error(err)
			}
			if mode == "finalize-live" {
				f.stop("web")
			}
			f.eventuallyRemoved()
		})
	}
}

func TestStopDuringFinalizeKeepsCleanupObligation(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "delivery\n")
	e := f.start("web")
	events := &worktreeEvents{waits: make(chan struct{}, 10)}
	f.m.logger = slog.New(slog.NewTextHandler(events, nil))
	unlock := holdWorktreeGit(t, f)
	join := startLockedWorktree(t, unlock, func() error {
		_, err := f.m.FinalizeWorktree(f.wt)
		return err
	})
	select {
	case <-events.waits:
	case <-time.After(10 * time.Second):
		join()
		t.Fatal("real lock barrier not reached")
	}
	f.auditProcess(e)
	f.stop("web")
	advance(f.m.now().Add(24 * time.Hour))
	f.m.Tick()
	views := f.m.Snapshot()
	if len(views) != 1 || views[0].RetainedWorktree != f.wt.Path {
		t.Errorf("pending delivery lost ended record/GC protection: %+v", views)
	}
	if err := join(); err != nil {
		t.Fatal(err)
	}
	f.eventuallyRemoved()
}

// Return without a normal-body unlock/join. The registered cleanup must finish
// the real finalizer and its subsequent release before fixture destruction.
func TestWorktreeFinalizerCollectedOnEarlyReturn(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "delivery\n")
	f.start("web")
	events := &worktreeEvents{waits: make(chan struct{}, 10)}
	f.m.logger = slog.New(slog.NewTextHandler(events, nil))
	unlock := holdWorktreeGit(t, f)
	finished := make(chan struct{})
	// Registered before the producer owner, hence checked after its Cleanup.
	t.Cleanup(func() {
		select {
		case <-finished:
		default:
			t.Fatal("fixture cleanup did not join the real finalizer")
		}
		f.removed()
	})
	startLockedWorktree(t, unlock, func() error {
		defer close(finished)
		_, err := f.m.FinalizeWorktree(f.wt)
		return err
	})
	select {
	case <-events.waits:
	case <-time.After(10 * time.Second):
		t.Fatal("real lock barrier not reached")
	}
	// Stop must finish with the Git lock still held; the finalizer then owns
	// the release. No Tick, future retry, or simulated completion is needed.
	f.stop("web")
}

func TestStartRechecksConfigReadBeforeCheckoutRemoval(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "delivery\n")
	// The ignored directory avoids changing finalization's saved tree. A real
	// FIFO holds FindConfig's original ReadFile before EOF, without a hook.
	dir := filepath.Join(f.wt.Path, ".dependencies", "config-read")
	config := filepath.Join(dir, ConfigPath)
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(config, 0600); err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { _, err := f.m.Start(f.task, dir, "web", os.Getenv("PATH")); started <- err }()
	writer, err := os.OpenFile(config, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, fmt.Sprintf("[environments.web]\nstart = %q\n", helperCommand("serve"))); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	// Writer open proves the original reader has the FIFO open. EOF remains
	// withheld while the real finalizer removes the checkout.
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		writer.Close()
		<-started
		t.Fatal(err)
	}
	f.removed()
	writer.Close()
	if err := <-started; err == nil {
		t.Fatal("stale config published an environment after removal")
	}
	if got := f.m.List(f.task); len(got) != 0 {
		t.Fatalf("stale config left records: %+v", got)
	}
}

func TestFinalizeFailureReleasesReservation(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	f.wt.AbortWithReason(errors.New("synthetic task abort"))
	if _, err := f.m.FinalizeWorktree(f.wt); err == nil {
		t.Fatal("aborted finalization succeeded")
	}
	if f.m.RetainsWorktree(f.root) {
		t.Fatal("failed finalization stranded its reservation")
	}
	e := f.start("web")
	f.present(e)
	f.stop("web")
	if _, err := os.Stat(f.wt.Path); err != nil {
		t.Fatalf("failed delivery was cleaned: %v", err)
	}
}
