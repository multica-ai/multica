package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// blockingChildEnv marks a re-executed copy of this test binary as the
// stand-in daemon process rather than a normal test run.
const blockingChildEnv = "MULTICA_TEST_BLOCKING_CHILD"

// TestBlockingChildProcess is not a test. It is the entry point the child from
// startBlockingChild runs, and it returns immediately unless that child's env
// var is set, so an ordinary `go test` sees a no-op.
//
// Re-executing the test binary rather than spawning `sleep` keeps this working
// on Windows, which ships no sleep.exe for exec.Command to resolve. This file
// carries no platform build tag, so `go test ./cmd/multica` has to pass there
// even though CI currently only builds the package on Windows.
func TestBlockingChildProcess(t *testing.T) {
	if os.Getenv(blockingChildEnv) != "1" {
		return
	}
	// Bounded rather than blocking forever: if the parent ever fails to clean
	// up, the stray process still exits instead of outliving the test run.
	time.Sleep(60 * time.Second)
}

// startBlockingChild returns the PID of a live process this test owns, for
// cases where the code under test may signal the PID it is handed.
func startBlockingChild(t *testing.T) int {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestBlockingChildProcess$")
	child.Env = append(os.Environ(), blockingChildEnv+"=1")
	if err := child.Start(); err != nil {
		t.Fatalf("start stand-in daemon process: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	return child.Process.Pid
}

// stubDaemon is a fake daemon on a real health port. It serves /shutdown as
// well as /health so `daemon stop` can complete through its graceful path:
// without that endpoint stop falls back to process.Kill() on whatever PID the
// health body advertises, which for a hardcoded PID means signalling an
// unrelated process on the developer's machine.
type stubDaemon struct {
	port int

	mu        sync.Mutex
	stopped   bool
	shutdowns int
}

func (s *stubDaemon) shutdownCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdowns
}

// serveHealthAs stands up a stub daemon on the health port that `profile`
// hashes to, answering with the identity `reports` claims. A nil reports map
// means a pre-#6694 daemon: alive, but unable to say who it is.
//
// pid is what the stub advertises. Tests that reach the kill path must pass a
// process they own; the identity tests never get that far, so they leave it at
// the current process's PID, which is never signalled because they stop at the
// refusal.
func serveHealthAs(t *testing.T, profile string, reports map[string]any) *stubDaemon {
	t.Helper()
	return serveHealthAsPID(t, profile, reports, os.Getpid())
}

func serveHealthAsPID(t *testing.T, profile string, reports map[string]any, pid int) *stubDaemon {
	t.Helper()
	port := healthPortForProfile(profile)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("health port %d for profile %q is busy on this machine: %v", port, profile, err)
	}
	stub := &stubDaemon{port: port}

	body := map[string]any{"status": "running", "pid": pid, "uptime": "1h0m0s", "cli_version": "v0.4.23"}
	for k, v := range reports {
		body[k] = v
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stopped := stub.stopped
		stub.mu.Unlock()
		if stopped {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "stopped"})
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		stub.mu.Lock()
		stub.stopped = true
		stub.shutdowns++
		stub.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return stub
}

func TestDaemonIdentityMismatch(t *testing.T) {
	cases := []struct {
		name    string
		health  map[string]any
		profile string
		wantErr bool
	}{
		{"same named profile", map[string]any{"profile": "dev"}, "dev", false},
		{"default profile identifying itself", map[string]any{"profile": ""}, "", false},
		{"different profile on our port", map[string]any{"profile": "dev-b"}, "dev-a", true},
		{"our port serving the default daemon", map[string]any{"profile": ""}, "dev", true},
		{"we asked for default, a named daemon answered", map[string]any{"profile": "dev"}, "", true},
		// A daemon that predates the field cannot prove anything; refusing
		// would break lifecycle commands against a merely older daemon.
		{"older daemon omits the field", map[string]any{"pid": 1.0}, "dev", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := daemonIdentityMismatch(tc.health, tc.profile, 19589)
			var mismatch *daemonProfileMismatchError
			if got := errors.As(err, &mismatch); got != tc.wantErr {
				t.Fatalf("daemonIdentityMismatch = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestDaemonProfileMismatchErrorNamesBothSides(t *testing.T) {
	err := &daemonProfileMismatchError{Want: "desktop-localhost-18310", Got: "desktop-localhost-18130", Port: 19589}
	msg := err.Error()
	for _, want := range []string{"19589", "desktop-localhost-18130", "desktop-localhost-18310", "same health port"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %q so the user can tell the two apart", msg, want)
		}
	}

	// The default profile has no name to print; "" would read as a bug.
	def := &daemonProfileMismatchError{Want: "dev", Got: "", Port: 19514}
	if !strings.Contains(def.Error(), "the default profile") {
		t.Errorf("error %q should name the default profile in words", def.Error())
	}
}

// The collision is only dangerous because these commands act on whatever
// answered: stop reads the PID off it and kills it, restart kills it and takes
// its port. Both must refuse before touching anything.
func TestDaemonLifecycleRefusesForeignDaemon(t *testing.T) {
	cases := []struct {
		name string
		run  func(*cobra.Command) error
	}{
		{"stop", func(c *cobra.Command) error { return runDaemonStop(c, nil) }},
		{"restart", func(c *cobra.Command) error { return runDaemonRestart(c, nil) }},
		{"start", func(c *cobra.Command) error { return runDaemonBackground(c) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearDaemonTaskEnv(t)
			mkProfiles(t, "collide-ab", "collide-ba")
			// Same bytes, so the same hashed port: the shape of a real
			// collision without depending on a lucky pair of names.
			serveHealthAs(t, "collide-ab", map[string]any{"profile": "collide-ab"})

			cmd := daemonStatusCmdFor(t, "collide-ba", "")
			cmd.Flags().Bool("foreground", false, "")
			err := tc.run(cmd)

			var mismatch *daemonProfileMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("daemon %s = %v, want a refusal naming the port collision", tc.name, err)
			}
			if mismatch.Got != "collide-ab" || mismatch.Want != "collide-ba" {
				t.Fatalf("mismatch = %+v, want got=collide-ab want=collide-ba", mismatch)
			}
		})
	}
}

// A daemon too old to identify itself must keep working: stop runs to
// completion against it rather than refusing.
//
// The stub advertises a process this test owns and started, so even if stop
// ever fell through to its forced-kill branch the signal could only reach that
// child — never an unrelated process that happens to hold the same PID.
func TestDaemonStopAcceptsDaemonWithoutProfileField(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t, "legacy")

	stub := serveHealthAsPID(t, "legacy", nil, startBlockingChild(t))

	err := runDaemonStop(daemonStatusCmdFor(t, "legacy", ""), nil)
	if err != nil {
		t.Fatalf("runDaemonStop = %v, want nil: a daemon that predates the profile field must not be refused", err)
	}
	// Asserting the outcome, not just the absence of a refusal: stop must have
	// gone through the graceful endpoint, which is also what proves it never
	// reached the branch that signals a PID.
	if got := stub.shutdownCount(); got != 1 {
		t.Fatalf("shutdown requests = %d, want 1 — stop should have shut the daemon down gracefully", got)
	}
}

func TestDaemonStatusReportsPortConflictInsteadOfClaimingItsOwn(t *testing.T) {
	t.Run("table says stopped and names the occupant", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "collide-ab", "collide-ba")
		stub := serveHealthAs(t, "collide-ab", map[string]any{"profile": "collide-ab"})

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "collide-ba", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		if !strings.Contains(out, "stopped") {
			t.Errorf("stdout = %q, want collide-ba reported as stopped — its daemon is not running", out)
		}
		if strings.Contains(out, "running (pid 4242") {
			t.Errorf("stdout = %q, must not report another profile's daemon as this one's", out)
		}
		if !strings.Contains(out, "collide-ab") || !strings.Contains(out, fmt.Sprint(stub.port)) {
			t.Errorf("stdout = %q, should name the occupying profile and the shared port", out)
		}
	})

	t.Run("json keeps status stopped and adds the conflict", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "collide-ab", "collide-ba")
		serveHealthAs(t, "collide-ab", map[string]any{"profile": "collide-ab"})

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "collide-ba", "json"), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		var payload struct {
			Status       string `json:"status"`
			PortConflict struct {
				Port    int    `json:"port"`
				Profile string `json:"profile"`
			} `json:"port_conflict"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
		}
		// Existing `jq -r .status` callers must keep reading a truthful value.
		if payload.Status != "stopped" {
			t.Errorf("status = %q, want stopped", payload.Status)
		}
		if payload.PortConflict.Profile != "collide-ab" {
			t.Errorf("port_conflict.profile = %q, want collide-ab", payload.PortConflict.Profile)
		}
	})
}

func TestDaemonStatusShowsWhoManagesTheDaemon(t *testing.T) {
	t.Run("desktop-managed daemon says so", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "desk-managed-test")
		serveHealthAs(t, "desk-managed-test", map[string]any{
			"profile": "desk-managed-test", "launched_by": "desktop",
		})

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "desk-managed-test", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		if !strings.Contains(out, "Managed by") || !strings.Contains(out, "Desktop") {
			t.Errorf("stdout = %q, want it to say the Desktop app manages this daemon", out)
		}
	})

	t.Run("standalone daemon output is unchanged", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "standalone")
		serveHealthAs(t, "standalone", map[string]any{"profile": "standalone"})

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "standalone", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		if strings.Contains(out, "Managed by") {
			t.Errorf("stdout = %q, a standalone daemon must not grow a Managed by row", out)
		}
	})
}

// Inside a daemon-managed task the health port is injected by the host daemon
// rather than derived from a profile hash, so there is no collision to detect.
// The task's own profile is necessarily empty (--profile is rejected there),
// so verifying identity would compare that empty profile against a named-
// profile host and report a phantom conflict — breaking the standing contract
// that `daemon status` in a task reports on the daemon hosting it.
func TestDaemonStatusInTaskContextReportsNamedProfileHost(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		clearDaemonTaskEnv(t)
		mkProfiles(t)
		stub := serveHealthAs(t, "host-named-profile", map[string]any{
			"profile": "host-named-profile", "launched_by": "desktop",
		})
		t.Setenv("MULTICA_TASK_ID", "task-test")
		t.Setenv("MULTICA_DAEMON_PORT", strconv.Itoa(stub.port))
	}

	t.Run("table reports the host as running", func(t *testing.T) {
		setup(t)
		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		if !strings.Contains(out, "running") {
			t.Fatalf("stdout = %q, want the hosting daemon reported as running", out)
		}
		if strings.Contains(out, "hashes to the same port") {
			t.Fatalf("stdout = %q, a named-profile host must not be reported as a port conflict", out)
		}
	})

	t.Run("json reports the host as running", func(t *testing.T) {
		setup(t)
		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "", "json"), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
		}
		if payload["status"] != "running" {
			t.Fatalf("status = %v, want running (host daemon)", payload["status"])
		}
		if _, ok := payload["port_conflict"]; ok {
			t.Fatalf("payload = %v, must not report a conflict against the injected host port", payload)
		}
	})
}

// A profile field that is present but not a string is a daemon answering
// wrongly, not an older daemon unable to answer. Collapsing it to "" would
// read as a match for the default profile and hand lifecycle commands a daemon
// whose identity was never established, so it fails closed.
func TestDaemonIdentityRejectsUnreadableProfileField(t *testing.T) {
	cases := []struct {
		name string
		raw  any
	}{
		{"null", nil},
		{"number", 42.0},
		{"object", map[string]any{"name": "dev"}},
		{"bool", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Target the default profile: that is where the dropped type check
			// used to turn an unreadable value into a silent match.
			err := daemonIdentityMismatch(map[string]any{"profile": tc.raw}, "", 19589)
			var mismatch *daemonProfileMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("daemonIdentityMismatch = %v, want a refusal", err)
			}
			if !mismatch.Unreadable {
				t.Fatalf("mismatch = %+v, want it flagged as an unreadable identity", mismatch)
			}
			if !strings.Contains(mismatch.Error(), "unreadable profile identity") {
				t.Errorf("error %q should say the identity could not be read", mismatch.Error())
			}
		})
	}
}

func TestDaemonRefusesDaemonWithUnreadableIdentity(t *testing.T) {
	t.Run("stop refuses", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "unreadable")
		serveHealthAs(t, "unreadable", map[string]any{"profile": nil})

		err := runDaemonStop(daemonStatusCmdFor(t, "unreadable", ""), nil)
		var mismatch *daemonProfileMismatchError
		if !errors.As(err, &mismatch) || !mismatch.Unreadable {
			t.Fatalf("runDaemonStop = %v, want a refusal on an unreadable identity", err)
		}
	})

	t.Run("status does not claim it", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "unreadable")
		serveHealthAs(t, "unreadable", map[string]any{"profile": nil})

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "unreadable", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonStatus = %v, want nil", err)
		}
		if strings.Contains(out, "running (pid") {
			t.Fatalf("stdout = %q, must not report an unverified daemon as this profile's", out)
		}
		if !strings.Contains(out, "could not be read") {
			t.Fatalf("stdout = %q, should explain that the identity could not be read", out)
		}
	})
}
