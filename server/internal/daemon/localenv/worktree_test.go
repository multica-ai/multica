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
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

type worktreeFixture struct {
	t      *testing.T
	repo   string
	root   string
	wt     *execenv.LocalWorktree
	m      *Manager
	task   Task
	params execenv.LocalWorktreeParams
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
	f := &worktreeFixture{t: t, repo: repo, root: root, wt: wt, m: NewManager(t.TempDir(), logger), task: Task{TaskID: params.TaskID, IssueID: params.ConversationID, WorkspaceID: params.WorkspaceID, RootDir: root}, params: params}
	// Stand in for dependencies installed inside the actual checkout.
	fixtureWrite(t, filepath.Join(wt.Path, ".dependencies/package/file"), "installed dependency\n")
	t.Cleanup(func() {
		for _, e := range f.m.List(f.task) {
			if e.State == StateRunning || e.State == StateStarting {
				f.stop(e.Name)
			}
		}
	})
	return f
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
			if err := restored.Load(); err != nil {
				t.Fatal(err)
			}
			f.m = restored
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
			// Waiter and stop goroutines both finish their cleanup under the Manager lock.
			f.m.Tick()
			f.removed()
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
	other.m = f.m
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
	lock, err := os.OpenFile(filepath.Join(f.repo, ".git", "multica-worktree.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entered := make(chan struct{}, 1)
	f.m.logger = slog.New(worktreeLockObserver{entered: entered})
	type result struct {
		out execenv.LocalWorktreeOutcome
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := f.m.FinalizeWorktree(f.wt); done <- result{out, err} }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		r := <-done
		t.Fatalf("production finalizer never reached lock barrier: %+v", r)
	}
	// Finalize now holds the Manager lock while the real Git lock is held here.
	// A concurrent Start must either observe the removed directory or fail its
	// actual cmd.Start, never publish a lease on the disposed checkout.
	startDone := make(chan error, 1)
	go func() { _, err := f.m.Start(f.task, f.wt.Path, "web", os.Getenv("PATH")); startDone <- err }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	r := <-done
	startErr := <-startDone
	if r.err != nil || r.out.RetainedWorktree != nil {
		t.Fatalf("completion: %+v %v", r.out, r.err)
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

func TestSharedEnvironmentRealTTLsReleaseOnlyTheLastCheckoutUser(t *testing.T) {
	f := newWorktreeFixture(t, "1m")
	data, err := os.ReadFile(filepath.Join(f.wt.Path, ConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	// Keep the second real expiry far enough apart to inspect the first release
	// without relying on scheduler timing or changing the Manager's clock.
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
		t.Logf("waiting for %s real TTL: %s", e.Name, e.ExpiresAt)
		for time.Now().Before(*e.ExpiresAt) {
			time.Sleep(250 * time.Millisecond)
		}
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
	expire(web)
	f.present(api)
	expire(api)
	f.removed()
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
