package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFindLocalDirectoryAssignment(t *testing.T) {
	const thisDaemon = "d-mine"
	otherDaemon := "d-other"

	mkRef := func(t *testing.T, ref localDirectoryRef) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(ref)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return raw
	}

	tmp := t.TempDir()

	t.Run("no resources returns nil", func(t *testing.T) {
		got, err := findLocalDirectoryAssignment(nil, thisDaemon)
		if err != nil || got != nil {
			t.Fatalf("expected (nil, nil), got (%+v, %v)", got, err)
		}
	})

	t.Run("other daemon is skipped", func(t *testing.T) {
		got, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp, DaemonID: otherDaemon})},
		}, thisDaemon)
		if err != nil || got != nil {
			t.Fatalf("expected (nil, nil), got (%+v, %v)", got, err)
		}
	})

	t.Run("non-matching type is skipped", func(t *testing.T) {
		got, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://x"}`)},
		}, thisDaemon)
		if err != nil || got != nil {
			t.Fatalf("expected (nil, nil), got (%+v, %v)", got, err)
		}
	})

	t.Run("matching daemon returns assignment", func(t *testing.T) {
		got, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp, DaemonID: thisDaemon})},
		}, thisDaemon)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got == nil {
			t.Fatalf("expected assignment, got nil")
		}
		if got.AbsPath != filepath.Clean(tmp) {
			t.Errorf("AbsPath = %q, want %q", got.AbsPath, filepath.Clean(tmp))
		}
		if got.RealPath == "" {
			t.Errorf("RealPath empty")
		}
	})

	t.Run("missing daemon_id is rejected", func(t *testing.T) {
		_, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp})},
		}, thisDaemon)
		if err == nil {
			t.Fatalf("expected error for missing daemon_id")
		}
	})

	t.Run("relative path is rejected", func(t *testing.T) {
		_, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: "relative/path", DaemonID: thisDaemon})},
		}, thisDaemon)
		if err == nil {
			t.Fatalf("expected error for relative path")
		}
	})

	t.Run("malformed ref json fails", func(t *testing.T) {
		_, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: json.RawMessage(`{not json`)},
		}, thisDaemon)
		if err == nil {
			t.Fatalf("expected error for malformed json")
		}
	})

	t.Run("two local_directory rows on this daemon fail fast", func(t *testing.T) {
		// Server-side findLocalDirectoryConflict enforces one
		// local_directory per (project, daemon). If two rows are
		// somehow present (older API client, direct DB writes), the
		// daemon must refuse to guess which directory to execute in.
		tmp2 := t.TempDir()
		_, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp, DaemonID: thisDaemon})},
			{ID: "r2", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp2, DaemonID: thisDaemon})},
		}, thisDaemon)
		if err == nil {
			t.Fatalf("expected error for two local_directory rows pinned to this daemon")
		}
		if !strings.Contains(err.Error(), "multiple local_directory") {
			t.Errorf("error %q did not mention multiple local_directory", err)
		}
	})

	t.Run("local_directory rows on different daemons coexist", func(t *testing.T) {
		// Different daemons MAY each carry one row — same path on
		// different machines is allowed; this daemon only resolves
		// its own row regardless of how many other-daemon rows are
		// in the list.
		got, err := findLocalDirectoryAssignment([]ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp, DaemonID: thisDaemon})},
			{ID: "r2", ResourceType: localDirectoryResourceType, ResourceRef: mkRef(t, localDirectoryRef{LocalPath: tmp, DaemonID: otherDaemon})},
		}, thisDaemon)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got == nil {
			t.Fatalf("expected assignment, got nil")
		}
	})
}

func TestAcquireLocalDirectoryLockSkipsSquadLeaderTasks(t *testing.T) {
	t.Parallel()

	const daemonID = "d-mine"
	tmp := t.TempDir()
	raw, err := json.Marshal(localDirectoryRef{LocalPath: tmp, DaemonID: daemonID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resources := []ProjectResourceData{
		{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: raw},
	}

	worker := Task{
		ID:               "worker-task",
		ProjectResources: resources,
	}
	assignment, err := localDirectoryAssignmentForTask(worker, daemonID)
	if err != nil {
		t.Fatalf("worker assignment: %v", err)
	}
	if assignment == nil {
		t.Fatal("worker assignment is nil")
	}

	d := &Daemon{
		cfg:            Config{DaemonID: daemonID},
		localPathLocks: NewLocalPathLocker(),
		logger:         slog.Default(),
	}
	leader := Task{
		ID:               "leader-task",
		IsLeaderTask:     true,
		ProjectResources: resources,
	}
	leaderAssignment, err := localDirectoryAssignmentForTask(leader, daemonID)
	if err != nil {
		t.Fatalf("leader assignment: %v", err)
	}
	if leaderAssignment != nil {
		t.Fatalf("leader assignment = %+v, want nil", leaderAssignment)
	}
	leaderRelease, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), leader, slog.Default())
	if abort {
		t.Fatal("leader lock acquisition aborted")
	}
	if leaderRelease != nil {
		t.Fatal("leader lock acquisition returned a release callback")
	}
	if got := d.localPathLocks.Holder(assignment.RealPath); got != "" {
		t.Fatalf("holder after leader skip = %q, want empty", got)
	}

	release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), worker, slog.Default())
	if abort {
		t.Fatal("worker lock acquisition aborted")
	}
	if release == nil {
		t.Fatal("worker lock acquisition returned nil release")
	}
	defer release()
	if got := d.localPathLocks.Holder(assignment.RealPath); got != worker.ID {
		t.Fatalf("holder = %q, want %q", got, worker.ID)
	}

	leaderRelease, abort = d.acquireLocalDirectoryLockIfNeeded(context.Background(), leader, slog.Default())
	if abort {
		t.Fatal("leader lock acquisition aborted")
	}
	if leaderRelease != nil {
		t.Fatal("leader lock acquisition returned a release callback")
	}
	if got := d.localPathLocks.Holder(assignment.RealPath); got != worker.ID {
		t.Fatalf("holder after leader skip = %q, want %q", got, worker.ID)
	}
}

func TestValidateLocalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("blacklist constants are POSIX-only in this test")
	}

	dir := t.TempDir()

	t.Run("accepts a writable directory", func(t *testing.T) {
		if err := validateLocalPath(dir); err != nil {
			t.Errorf("unexpected: %v", err)
		}
	})

	t.Run("rejects relative path", func(t *testing.T) {
		if err := validateLocalPath("relative"); err == nil {
			t.Errorf("expected error")
		}
	})

	t.Run("rejects empty path", func(t *testing.T) {
		if err := validateLocalPath(""); err == nil {
			t.Errorf("expected error")
		}
	})

	t.Run("rejects system roots", func(t *testing.T) {
		for _, banned := range []string{"/", "/Users", "/home"} {
			if err := validateLocalPath(banned); err == nil {
				t.Errorf("expected error for %q", banned)
			}
		}
	})

	t.Run("rejects the user home directory", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir")
		}
		if err := validateLocalPath(home); err == nil {
			t.Errorf("expected error for $HOME")
		}
	})

	t.Run("rejects missing path", func(t *testing.T) {
		missing := filepath.Join(dir, "does-not-exist")
		if err := validateLocalPath(missing); err == nil {
			t.Errorf("expected error")
		}
	})

	t.Run("rejects a regular file", func(t *testing.T) {
		f := filepath.Join(dir, "afile")
		if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := validateLocalPath(f); err == nil {
			t.Errorf("expected error")
		}
	})

	t.Run("rejects an unwritable directory", func(t *testing.T) {
		// chmod-based unwritable is unreliable as root; skip when uid==0.
		if os.Getuid() == 0 {
			t.Skip("test cannot run as root; chmod is a no-op")
		}
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o555); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := validateLocalPath(ro); err == nil {
			t.Errorf("expected error for read-only directory")
		}
	})

	t.Run("rejects a symlink pointing at the user home", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir")
		}
		link := filepath.Join(dir, "home-link")
		if err := os.Symlink(home, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		// The literal cleaned path is dir/home-link, which is NOT in the
		// blacklist. Without the realpath check this used to pass.
		err = validateLocalPath(link)
		if err == nil {
			t.Fatal("expected error for symlink pointing at $HOME")
		}
		if !strings.Contains(err.Error(), "user's home directory") {
			t.Errorf("error %q did not flag the home-dir reason", err.Error())
		}
	})

	t.Run("rejects a symlink pointing at a system root", func(t *testing.T) {
		link := filepath.Join(dir, "root-link")
		// Pick a banned system root that's predictably present on the
		// host. /Users on macOS; /home on Linux. Fall back to /etc which
		// is in the blacklist and exists on both.
		target := "/etc"
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		err := validateLocalPath(link)
		if err == nil {
			t.Fatal("expected error for symlink pointing at a system root")
		}
		if !strings.Contains(err.Error(), "protected system root") {
			t.Errorf("error %q did not flag the system-root reason", err.Error())
		}
	})

	t.Run("accepts a symlink to a non-blacklisted directory", func(t *testing.T) {
		target := filepath.Join(dir, "real-proj")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		link := filepath.Join(dir, "proj-link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if err := validateLocalPath(link); err != nil {
			t.Errorf("symlink to a regular directory should pass, got %v", err)
		}
	})

	// macOS aliases /tmp, /etc, /var to /private/{tmp,etc,var} via OS-level
	// symlinks. A user typing the canonical /private/... form in the picker
	// would pass the literal blacklist (it doesn't contain /private/tmp)
	// and EvalSymlinks would be a no-op (the input is already canonical),
	// so the old "only re-check when realPath != absPath" gate skipped it.
	// Cover the regression so the realpath blacklist always runs.
	t.Run("rejects canonical macOS /private/{tmp,etc,var}", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("macOS-only: /private/* aliases don't exist elsewhere")
		}
		for _, p := range []string{"/private/tmp", "/private/etc", "/private/var"} {
			if _, statErr := os.Stat(p); statErr != nil {
				t.Logf("skipping %q: %v", p, statErr)
				continue
			}
			err := validateLocalPath(p)
			if err == nil {
				t.Errorf("expected error for canonical %q", p)
				continue
			}
			if !strings.Contains(err.Error(), "protected system root") {
				t.Errorf("error %q for %q did not flag the system-root reason", err.Error(), p)
			}
		}
	})

}

// TestIsDriveRoot covers the Windows drive-root generalisation. Static
// enumeration in the old blacklist (C..F) missed mounts at G:\ and up; the
// new check goes through filepath.VolumeName so any drive letter (and UNC
// roots) is rejected.
func TestIsDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		// filepath.VolumeName returns "" on POSIX, so isDriveRoot always
		// returns false off Windows. The semantic contract is enforced by
		// the early `runtime.GOOS != "windows"` guard; the case table
		// below is only meaningful on a Windows runner.
		t.Skip("windows-only behaviour")
	}
	cases := []struct {
		p    string
		want bool
	}{
		{`C:\`, true},
		{`G:\`, true},
		{`Z:\`, true},
		{`C:/`, true},
		{`C:`, true},
		{`\\srv\share`, true},
		{`\\srv\share\`, true},
		{`C:\Users`, false},
		{`D:\proj`, false},
		{`C:\Users\me\code`, false},
	}
	for _, c := range cases {
		if got := isDriveRoot(c.p); got != c.want {
			t.Errorf("isDriveRoot(%q) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestLocalPathLockerSerializes(t *testing.T) {
	locker := NewLocalPathLocker()
	const path = "/some/path"

	rel1, err := locker.Acquire(context.Background(), path, "task-1", nil)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if got := locker.Holder(path); got != "task-1" {
		t.Errorf("holder = %q, want task-1", got)
	}

	// task-2 must wait, with onWait fired and the holder reported.
	var waitCalls atomic.Int32
	var sawHolder atomic.Value
	done := make(chan error, 1)
	go func() {
		rel, err := locker.Acquire(context.Background(), path, "task-2", func(holder string) {
			waitCalls.Add(1)
			sawHolder.Store(holder)
		})
		if err != nil {
			done <- err
			return
		}
		if got := locker.Holder(path); got != "task-2" {
			done <- errorsNew("holder after handover = " + got)
			return
		}
		rel()
		done <- nil
	}()

	// give the goroutine time to enter the wait
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && waitCalls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if waitCalls.Load() != 1 {
		t.Fatalf("onWait calls = %d, want 1", waitCalls.Load())
	}
	if got := sawHolder.Load(); got != "task-1" {
		t.Errorf("onWait holder = %v, want task-1", got)
	}

	rel1()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never woke")
	}
	if got := locker.Holder(path); got != "" {
		t.Errorf("holder after release = %q, want empty", got)
	}
}

func TestLocalPathLockerCtxCancel(t *testing.T) {
	locker := NewLocalPathLocker()
	const path = "/some/path"

	rel1, err := locker.Acquire(context.Background(), path, "task-1", nil)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	defer rel1()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = locker.Acquire(ctx, path, "task-2", nil)
	if err == nil {
		t.Fatalf("expected ctx error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestLocalPathLockerDistinctPathsParallel(t *testing.T) {
	locker := NewLocalPathLocker()

	rel1, err := locker.Acquire(context.Background(), "/a", "task-1", nil)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	defer rel1()

	// Different path must not block.
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rel2, err := locker.Acquire(context.Background(), "/b", "task-2", nil)
		if err != nil {
			t.Errorf("acquire 2: %v", err)
			return
		}
		rel2()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acquire on distinct path blocked")
	}
	wg.Wait()
}

// errorsNew is a tiny helper so the goroutine above can return a typed error
// without importing errors / fmt at the call site.
func errorsNew(msg string) error { return &waiterError{msg: msg} }

type waiterError struct{ msg string }

func (e *waiterError) Error() string { return e.msg }

// TestAcquireLocalDirectoryLock_CancelDuringWait covers the gap between
// dispatch and StartTask: while the path mutex is contended, the main
// per-task cancellation watcher hasn't started yet. If the issue is
// cancelled (or the task row is reassigned / deleted) during the wait,
// the daemon must notice promptly and bail — otherwise the slot stays
// pinned by a phantom waiter for the full lifetime of the holder.
func TestAcquireLocalDirectoryLock_CancelDuringWait(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() // valid, writable, non-blacklisted
	// Pre-claim must use the same key the production path computes, which
	// is the symlink-resolved realpath. On macOS, /tmp/... resolves to
	// /private/tmp/..., so a literal preclaim with `dir` would miss the
	// production key and the new acquire would win on the fast path.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}

	// Server-side state for the fake. Mark the task cancelled only after
	// we've seen the daemon call wait-local-directory, so the test can
	// assert the watcher reacted to the post-park cancel rather than
	// reading stale state on the very first poll.
	var (
		parked   atomic.Bool
		waitCall atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/wait-local-directory"):
			waitCall.Add(1)
			parked.Store(true)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			if parked.Load() {
				_, _ = w.Write([]byte(`{"status":"cancelled"}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}
		default:
			// We don't expect /fail in the cancel path — assert that
			// by failing loud if it gets called.
			t.Errorf("unexpected daemon call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	const daemonID = "d-test"
	const heldByTaskID = "task-holder"
	const newTaskID = "task-waiter"

	locker := NewLocalPathLocker()
	// Pre-claim the lock so the new task has to wait. Use the resolved
	// realpath as the key to match findLocalDirectoryAssignment.
	release, err := locker.Acquire(context.Background(), realDir, heldByTaskID, nil)
	if err != nil {
		t.Fatalf("preclaim acquire: %v", err)
	}
	t.Cleanup(release)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.Default(),
		localPathLocks:     locker,
		cancelPollInterval: 10 * time.Millisecond,
		cfg:                Config{DaemonID: daemonID},
	}

	ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: daemonID})
	if err != nil {
		t.Fatalf("marshal ref: %v", err)
	}
	task := Task{
		ID: newTaskID,
		ProjectResources: []ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: ref},
		},
	}

	type result struct {
		release func()
		abort   bool
	}
	done := make(chan result, 1)
	go func() {
		rel, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), task, slog.Default())
		done <- result{release: rel, abort: abort}
	}()

	select {
	case got := <-done:
		if !got.abort {
			t.Fatal("expected abort=true after server-side cancel, got abort=false")
		}
		if got.release != nil {
			t.Fatal("expected nil release on cancel, got a non-nil callback")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquireLocalDirectoryLockIfNeeded blocked past 2s — cancel was not observed during wait")
	}

	if got := waitCall.Load(); got != 1 {
		t.Errorf("wait-local-directory calls = %d, want 1", got)
	}
}

func TestAcquireLocalDirectoryLock_ParentCancellationReportsWaitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}

	parked := make(chan struct{}, 1)
	var failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/wait-local-directory"):
			select {
			case parked <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected daemon call: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	const daemonID = "d-test"
	locker := NewLocalPathLocker()
	release, err := locker.Acquire(context.Background(), realDir, "task-holder", nil)
	if err != nil {
		t.Fatalf("preclaim acquire: %v", err)
	}
	t.Cleanup(release)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.Default(),
		localPathLocks:     locker,
		cancelPollInterval: time.Hour,
		cfg:                Config{DaemonID: daemonID},
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: daemonID})
	if err != nil {
		t.Fatalf("marshal ref: %v", err)
	}
	task := Task{
		ID: "task-waiter",
		ProjectResources: []ProjectResourceData{
			{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: ref},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	type result struct {
		release func()
		abort   bool
	}
	done := make(chan result, 1)
	go func() {
		rel, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, task, slog.Default())
		done <- result{release: rel, abort: abort}
	}()

	select {
	case <-parked:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("task never entered local_directory wait")
	}

	select {
	case got := <-done:
		if !got.abort {
			t.Fatal("expected abort=true after parent cancellation")
		}
		if got.release != nil {
			t.Fatal("expected nil release after parent cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquireLocalDirectoryLockIfNeeded did not return after parent cancellation")
	}

	if got := failCalls.Load(); got != 1 {
		t.Fatalf("fail callback calls = %d, want 1", got)
	}
}

func TestAcquireLocalDirectoryLock_EarlyFailureReportsWithCancelledParent(t *testing.T) {
	t.Parallel()

	var failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/fail") {
			t.Errorf("unexpected daemon call: %s %s", req.Method, req.URL.Path)
		}
		failCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	const daemonID = "d-test"
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.Default(),
		localPathLocks: NewLocalPathLocker(),
		cfg:            Config{DaemonID: daemonID},
	}
	missingPathRef, err := json.Marshal(localDirectoryRef{
		LocalPath: filepath.Join(t.TempDir(), "missing"),
		DaemonID:  daemonID,
	})
	if err != nil {
		t.Fatalf("marshal ref: %v", err)
	}
	tests := []struct {
		name string
		ref  json.RawMessage
	}{
		{name: "resource resolution", ref: json.RawMessage(`{not-json`)},
		{name: "path validation", ref: missingPathRef},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := Task{
				ID: "task-invalid-local-directory",
				ProjectResources: []ProjectResourceData{
					{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: tc.ref},
				},
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			release, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, task, slog.Default())
			if !abort {
				t.Fatal("expected local_directory failure to abort task")
			}
			if release != nil {
				t.Fatal("expected nil release on local_directory failure")
			}
		})
	}
	if got := failCalls.Load(); got != int32(len(tests)) {
		t.Fatalf("fail callback calls = %d, want %d", got, len(tests))
	}
}
