package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAcquireOwnership_SecondFailsWhileHeld covers simultaneous startup /
// duplicate identity: a second acquirer of the same base dir is rejected while
// the first holds the lock. flock treats two independent opens of the same
// file as contending even within one process (flock(2)), so this is a faithful
// stand-in for two daemon processes racing the same machine lock.
func TestAcquireOwnership_SecondFailsWhileHeld(t *testing.T) {
	baseDir := t.TempDir()

	first, err := AcquireOwnership(baseDir, OwnerInfo{PID: 4242, HealthPort: 19514, DaemonID: "d-1", Version: "0.4.2", Profile: "", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	_, err = AcquireOwnership(baseDir, OwnerInfo{PID: 5555, HealthPort: 19999, DaemonID: "d-2", Version: "0.4.2", Profile: "desktop-host", StartedAt: time.Now()})
	var conflict *OwnershipConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("second acquire: want *OwnershipConflict, got %v", err)
	}
	if !conflict.HasInfo || conflict.Incumbent.PID != 4242 {
		t.Fatalf("conflict should name the incumbent (pid 4242): %+v", conflict.Incumbent)
	}
	// The message must be actionable — name the stop and takeover remedies.
	msg := conflict.Error()
	for _, want := range []string{"daemon stop", "--takeover", baseDir} {
		if !strings.Contains(msg, want) {
			t.Fatalf("conflict message missing %q: %s", want, msg)
		}
	}
}

// TestOwnershipConflict_ProfileAwareStopHint pins that the stop remedy targets
// the OWNER's profile: a bare `multica daemon stop` only reaches the default
// profile's daemon, which is exactly the cross-profile blindness the lock
// exists to catch.
func TestOwnershipConflict_ProfileAwareStopHint(t *testing.T) {
	withProfile := &OwnershipConflict{
		Path:      "/tmp/x/daemon.lock",
		Incumbent: OwnerInfo{PID: 1, Profile: "desktop-host", HealthPort: 20000},
		HasInfo:   true,
	}
	if !strings.Contains(withProfile.Error(), "`multica --profile desktop-host daemon stop`") {
		t.Fatalf("named-profile incumbent must get a profile-scoped stop hint: %s", withProfile.Error())
	}

	defaultProfile := &OwnershipConflict{
		Path:      "/tmp/x/daemon.lock",
		Incumbent: OwnerInfo{PID: 1, Profile: "", HealthPort: 19514},
		HasInfo:   true,
	}
	msg := defaultProfile.Error()
	if !strings.Contains(msg, "`multica daemon stop`") || strings.Contains(msg, "--profile ") {
		t.Fatalf("default-profile incumbent must get the plain stop hint: %s", msg)
	}
}

// TestAcquireOwnership_ReleaseAllowsReacquire covers clean shutdown: after the
// owner releases, the next daemon acquires immediately.
func TestAcquireOwnership_ReleaseAllowsReacquire(t *testing.T) {
	baseDir := t.TempDir()

	first, err := AcquireOwnership(baseDir, OwnerInfo{PID: 1, Version: "0.4.2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Release is idempotent / nil-safe.
	if err := first.Release(); err != nil {
		t.Fatalf("second release should be a no-op: %v", err)
	}

	second, err := AcquireOwnership(baseDir, OwnerInfo{PID: 2, Version: "0.4.2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("reacquire after clean release: %v", err)
	}
	defer second.Release()

	// The body should now describe the new owner.
	info, ok := ReadOwnerInfo(baseDir)
	if !ok || info.PID != 2 {
		t.Fatalf("lock body should name the new owner (pid 2): ok=%v info=%+v", ok, info)
	}
}

// TestAcquireOwnership_StaleLockFileReclaimed covers stale-lease recovery: a
// leftover lock file from a dead owner (content present, no live lock) must not
// block a new daemon, and requires no manual file surgery.
func TestAcquireOwnership_StaleLockFileReclaimed(t *testing.T) {
	baseDir := t.TempDir()

	stale := OwnerInfo{PID: 999999, HealthPort: 19514, DaemonID: "dead", Version: "0.3.0", StartedAt: time.Now().Add(-time.Hour)}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(OwnershipLockPath(baseDir), data, 0o644); err != nil {
		t.Fatalf("seed stale lock file: %v", err)
	}

	lock, err := AcquireOwnership(baseDir, OwnerInfo{PID: os.Getpid(), Version: "0.4.2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("stale leftover lock file must not block acquire: %v", err)
	}
	defer lock.Release()

	info, ok := ReadOwnerInfo(baseDir)
	if !ok || info.PID != os.Getpid() {
		t.Fatalf("acquire should overwrite the stale body with our own: ok=%v info=%+v", ok, info)
	}
}

// TestProbeOwnership_FreeAndHeld covers the launcher probe used to give a fast
// conflict message (and drive takeover) without recording a new owner.
func TestProbeOwnership_FreeAndHeld(t *testing.T) {
	baseDir := t.TempDir()

	free, _, _, err := ProbeOwnership(baseDir)
	if err != nil {
		t.Fatalf("probe (free): %v", err)
	}
	if !free {
		t.Fatal("probe should report a fresh base dir as free")
	}
	// Probing must NOT have recorded an owner.
	if _, ok := ReadOwnerInfo(baseDir); ok {
		t.Fatal("probe must not write an owner record")
	}

	lock, err := AcquireOwnership(baseDir, OwnerInfo{PID: 7, HealthPort: 20200, Version: "0.4.2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lock.Release()

	free, incumbent, ok, err := ProbeOwnership(baseDir)
	if err != nil {
		t.Fatalf("probe (held): %v", err)
	}
	if free {
		t.Fatal("probe should report a held lock as not free")
	}
	if !ok || incumbent.PID != 7 || incumbent.HealthPort != 20200 {
		t.Fatalf("probe should surface the incumbent (pid 7, port 20200): ok=%v %+v", ok, incumbent)
	}
}

// TestOwnershipBypassed covers the break-glass / rollback env parsing.
func TestOwnershipBypassed(t *testing.T) {
	cases := []struct {
		val  string
		set  bool
		want bool
	}{
		{set: false, want: false},
		{val: "", set: true, want: false},
		{val: "0", set: true, want: false},
		{val: "false", set: true, want: false},
		{val: "FALSE", set: true, want: false},
		{val: "1", set: true, want: true},
		{val: "true", set: true, want: true},
		{val: "yes", set: true, want: true},
	}
	for _, tc := range cases {
		if tc.set {
			t.Setenv("MULTICA_DAEMON_ALLOW_MULTIPLE", tc.val)
		} else {
			os.Unsetenv("MULTICA_DAEMON_ALLOW_MULTIPLE")
		}
		if got := OwnershipBypassed(); got != tc.want {
			t.Fatalf("OwnershipBypassed(set=%v val=%q)=%v, want %v", tc.set, tc.val, got, tc.want)
		}
	}
}

// TestAcquireOwnership_CrashRecovery covers crash recovery across REAL OS
// processes: a helper process acquires the lock, is SIGKILLed (no clean
// Release runs), and the parent must then acquire immediately — proving the
// kernel drops the advisory lock on process death with no stale window and no
// manual cleanup. This subprocess test is also the end-to-end cross-process
// reproduction (a two-goroutine test cannot distinguish the fixed lock from the
// old process-local mutex). It is fully isolated: a temp base dir, no health
// port, no server.
func TestAcquireOwnership_CrashRecovery(t *testing.T) {
	baseDir := t.TempDir()
	flag := filepath.Join(baseDir, "acquired.flag")

	helper := exec.Command(os.Args[0], "-test.run=TestOwnershipHelperProcess")
	helper.Env = append(os.Environ(),
		"MULTICA_OWNERSHIP_HELPER=1",
		"MULTICA_OWNERSHIP_BASEDIR="+baseDir,
	)
	helper.Stdout = os.Stderr // fold helper test output into the parent log
	helper.Stderr = os.Stderr
	if err := helper.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
	})

	// Wait for the helper to signal it holds the lock.
	if !waitForFile(flag, 10*time.Second) {
		t.Fatal("helper did not acquire the lock within 10s")
	}

	// While the helper holds it, the parent cannot acquire.
	if _, err := AcquireOwnership(baseDir, OwnerInfo{PID: os.Getpid(), Version: "0.4.2", StartedAt: time.Now()}); err == nil {
		t.Fatal("parent acquired while helper still held the lock")
	}

	// Crash the helper (SIGKILL — no defer, no Release).
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = helper.Process.Wait()

	// The OS should have released the lock on death; the parent acquires,
	// retrying briefly to absorb kernel reap latency.
	var (
		lock *OwnershipLock
		err  error
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		lock, err = AcquireOwnership(baseDir, OwnerInfo{PID: os.Getpid(), Version: "0.4.2", StartedAt: time.Now()})
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("crash of the owner should have freed the lock: %v", err)
	}
	_ = lock.Release()
}

// TestOwnershipHelperProcess is the child-process entrypoint for
// TestAcquireOwnership_CrashRecovery. It runs only when invoked with the helper
// env var set; otherwise it is a no-op so the normal suite does not spawn
// anything.
func TestOwnershipHelperProcess(t *testing.T) {
	if os.Getenv("MULTICA_OWNERSHIP_HELPER") != "1" {
		t.Skip("child-process entrypoint; not run directly")
	}
	baseDir := os.Getenv("MULTICA_OWNERSHIP_BASEDIR")
	hp, _ := strconv.Atoi(os.Getenv("MULTICA_OWNERSHIP_HEALTHPORT"))
	lock, err := AcquireOwnership(baseDir, OwnerInfo{
		PID: os.Getpid(), HealthPort: hp, DaemonID: "helper", Version: "9.9.9", StartedAt: time.Now(),
	})
	if err != nil {
		// Signal failure by leaving the flag absent; parent times out.
		os.Exit(3)
	}
	// Signal "acquired" via a sentinel file the parent polls for.
	_ = os.WriteFile(filepath.Join(baseDir, "acquired.flag"), []byte("ok"), 0o644)
	// Hold until killed, with a safety cap so a leaked helper self-exits.
	time.Sleep(60 * time.Second)
	_ = lock.Release()
	os.Exit(0)
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
