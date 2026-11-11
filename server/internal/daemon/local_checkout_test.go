package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestLocalCheckoutConcurrentAndRepeatedRequests(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	wt := &execenv.LocalWorktree{WorkDir: "private", Branch: "agent/test", BaseCommit: "abc"}
	s := newLocalCheckout(context.Background(), func(context.Context) (*execenv.LocalWorktree, error) {
		calls.Add(1)
		close(started)
		<-release
		return wt, nil
	})
	t.Cleanup(func() { s.close() })
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.checkout(context.Background())
			if err != nil || got != wt {
				t.Errorf("checkout = %v, %v", got, err)
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("prepared %d worktrees", calls.Load())
	}
	if got, err := s.checkout(context.Background()); err != nil || got != wt {
		t.Fatalf("retry = %v, %v", got, err)
	}
	if s.close() != wt {
		t.Fatal("close lost prepared worktree")
	}
	if _, err := s.checkout(context.Background()); !errors.Is(err, errLocalCheckoutClosed) {
		t.Fatalf("late checkout: %v", err)
	}
}

func TestLocalCheckoutCloseCancelsAndJoinsPreparation(t *testing.T) {
	t.Parallel()
	started, exited, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	s := newLocalCheckout(context.Background(), func(ctx context.Context) (*execenv.LocalWorktree, error) {
		close(started)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	go func() { _, err := s.checkout(context.Background()); result <- err }()
	<-started
	if s.close() != nil {
		t.Fatal("cancelled preparation produced a worktree")
	}
	select {
	case <-exited:
	default:
		t.Fatal("close returned before preparation exited")
	}
	if !errors.Is(<-result, context.Canceled) {
		t.Fatal("preparation did not observe cancellation")
	}
}

func TestLocalCheckoutFailureCanRetryAndUnusedRunDoesNothing(t *testing.T) {
	t.Parallel()
	calls := 0
	s := newLocalCheckout(context.Background(), func(context.Context) (*execenv.LocalWorktree, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("snapshot failed")
		}
		return &execenv.LocalWorktree{Branch: "retry"}, nil
	})
	if _, err := s.checkout(context.Background()); err == nil {
		t.Fatal("failure lost")
	}
	if wt, err := s.checkout(context.Background()); err != nil || wt.Branch != "retry" {
		t.Fatalf("retry: %v, %v", wt, err)
	}
	s.close()
	unused := newLocalCheckout(context.Background(), func(context.Context) (*execenv.LocalWorktree, error) {
		t.Fatal("unused run prepared a checkout")
		return nil, nil
	})
	if unused.close() != nil {
		t.Fatal("unused run has a worktree")
	}
}

func TestLocalCheckoutHandlerAuthAndBoundPath(t *testing.T) {
	t.Parallel()
	d := &Daemon{logger: slog.Default()}
	s := newLocalCheckout(context.Background(), func(context.Context) (*execenv.LocalWorktree, error) {
		return &execenv.LocalWorktree{WorkDir: "bound-private-path", Branch: "agent/owned", BaseCommit: "abc", SnapshotAt: time.Now()}, nil
	})
	t.Cleanup(func() { s.close() })
	d.registerActiveRepoCheckoutTask("active", activeRepoCheckoutTask{LocalCheckout: s})
	d.registerActiveRepoCheckoutTask("other", activeRepoCheckoutTask{})
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {"invalid", 401}, {"other", 409}, {"active", 200}, {"active", 200}} {
		req := httptest.NewRequest("POST", "/local-directory/checkout", strings.NewReader(`{"local_path":"/someone/else","workdir":"/not/owned"}`))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rec := httptest.NewRecorder()
		d.localCheckoutHandler().ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%q: %d: %s", tc.token, rec.Code, rec.Body.String())
		}
		if tc.want == 200 && !strings.Contains(rec.Body.String(), "bound-private-path") {
			t.Fatal("caller chose checkout path")
		}
	}
	// A copied activeTask can outlive removal from the authentication map.
	s.close()
	if _, err := s.checkout(context.Background()); !errors.Is(err, errLocalCheckoutClosed) {
		t.Fatal("authenticated request bypassed close")
	}
	d.clearActiveRepoCheckoutTask("active")
	req := httptest.NewRequest("POST", "/local-directory/checkout", nil)
	req.Header.Set("Authorization", "Bearer active")
	rec := httptest.NewRecorder()
	d.localCheckoutHandler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("finished task accepted: %d", rec.Code)
	}
}

func TestLocalCheckoutWaitForInPlaceLockIsCancellable(t *testing.T) {
	t.Parallel()
	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.localPathLocks = NewLocalPathLocker()
	repo := createDaemonTestRepo(t)
	a := &localDirectoryAssignment{Ref: localDirectoryRef{ExecutionMode: "worktree"}, AbsPath: repo, RealPath: repo}
	release, err := d.localPathLocks.Acquire(context.Background(), repo, "holder", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	s := d.taskLocalCheckout(ctx, leaderReuseTestTask("waiter"), a, &execenv.Environment{RootDir: t.TempDir()})
	result := make(chan error, 1)
	go func() { _, err := s.checkout(context.Background()); result <- err }()
	cancel()
	s.close()
	if err := <-result; err == nil {
		t.Fatal("cancelled waiter created a worktree")
	}
}

func TestRunTaskLocalWorktreeOnDemandAcrossReadOnlyTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake agent")
	}
	t.Parallel()
	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.cfg.DaemonID = "local-daemon"
	d.localPathLocks = NewLocalPathLocker()
	repo := createDaemonTestRepo(t)
	gitOK(t, repo, "config", "user.name", "Test")
	gitOK(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "user.txt"), []byte("user draft"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceStatus := gitOK(t, repo, "status", "--porcelain")
	mux := http.NewServeMux()
	mux.HandleFunc("/local-directory/checkout", d.localCheckoutHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	d.cfg.HealthPort, _ = strconv.Atoi(port)
	control := filepath.Join(t.TempDir(), "write")
	script := fmt.Sprintf(`#!/bin/sh
IFS= read -r _
[ ! -e ../worktree ] || exit 1
if [ -f %q ]; then
  curl -fsS -X POST -H "Authorization: Bearer $MULTICA_TOKEN" "http://127.0.0.1:$MULTICA_DAEMON_PORT/local-directory/checkout" >/dev/null || exit 1
  cat %q > ../worktree/agent.txt
  git -C ../worktree add agent.txt || exit 1
  git -C ../worktree -c commit.gpgSign=false commit -qm 'agent change' || exit 1
fi
printf '%%s\n' '{"type":"system","session_id":"session-local"}'
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-local","result":"done"}'
`, control, control)
	writeTestExecutable(t, d.cfg.Agents["claude"].Path, []byte(script))
	task := leaderReuseTestTask("local-first")
	task.IsLeaderTask = false
	task.IssueIdentifier = "MUL-8038"
	ref, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: "worktree"})
	task.ProjectResources = []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: ref}}
	if err := os.WriteFile(control, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || first.BranchName == "" {
		t.Fatalf("first: %+v, %v", first, err)
	}
	if execenv.HasLocalWorktree(first.EnvRoot) {
		t.Fatal("finalized worktree remains")
	}
	if got := gitOK(t, repo, "show", first.BranchName+":agent.txt"); got != "first" {
		t.Fatalf("delivery: %q", got)
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	task.ID, task.PriorWorkDir, task.PriorSessionID = "local-read-only", first.WorkDir, first.SessionID
	second, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || second.BranchName != "" || !sameDir(t, first.WorkDir, second.WorkDir) {
		t.Fatalf("read-only: %+v, %v", second, err)
	}
	if err := os.WriteFile(control, []byte("third"), 0o644); err != nil {
		t.Fatal(err)
	}
	task.ID, task.PriorWorkDir, task.PriorSessionID = "local-third", second.WorkDir, second.SessionID
	third, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || third.BranchName != first.BranchName {
		t.Fatalf("third: %+v, %v", third, err)
	}
	if got := gitOK(t, repo, "show", third.BranchName+":agent.txt"); got != "third" {
		t.Fatalf("delivery: %q", got)
	}
	if got := gitOK(t, repo, "status", "--porcelain"); got != sourceStatus {
		t.Fatalf("user directory changed: %q", got)
	}
	// A failed finalization must survive both a follow-up and redispatch of
	// the task that owns the original managed root.
	params := execenv.LocalWorktreeParamsForRun(execenv.PrepareParams{
		WorkspaceID: task.WorkspaceID, TaskID: "preserved", AgentName: task.Agent.Name,
		IssueIdentifier: task.IssueIdentifier,
		Task:            execenv.TaskContextForEnv{IssueID: task.IssueID, AgentID: task.AgentID},
	}, third.EnvRoot)
	params.LocalPath, params.PreserveExisting = repo, true
	wt, err := execenv.PrepareLocalWorktree(params, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(wt.WorkDir, "precious.txt")
	if err := os.WriteFile(precious, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt.AbortWithReason(errors.New("process cleanup unconfirmed"))
	if _, err := wt.Finalize(d.logger); err == nil {
		t.Fatal("aborted worktree was removed")
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	task.ID, task.PriorWorkDir, task.PriorSessionID = "local-after-failure", third.WorkDir, third.SessionID
	fourth, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil || sameDir(t, fourth.WorkDir, third.WorkDir) {
		t.Fatalf("reused preserved worktree: %+v, %v", fourth, err)
	}
	task.ID = "local-first"
	if _, err := d.runTask(context.Background(), task, "claude", 0, d.logger); err == nil || !strings.Contains(err.Error(), "preserving") {
		t.Fatalf("redispatch overwrote preserved work: %v", err)
	}
	if data, err := os.ReadFile(precious); err != nil || string(data) != "keep me" {
		t.Fatalf("preserved work lost: %q, %v", data, err)
	}
}
