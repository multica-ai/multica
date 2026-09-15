package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDsh records every invocation and its PATH, then exits per the args it
// was handed. Failing on a spec containing FAILME is how the candidate
// fallback gets exercised.
//
// A successful `add` writes the profile manifest, because that is what the real
// command does and what the caller now judges success by. DSH_HOME is pinned
// first so the manifest lands in the test's own directory and never in the
// developer's ~/.dsh.
func fakeDsh(t *testing.T) (path, record string) {
	t.Helper()
	return fakeDshScript(t, `mkdir -p "$DSH_HOME/profiles/multica" && printf '{}' > "$DSH_HOME/profiles/multica/package.json"`)
}

// fakeDshSilentlySucceeds exits 0 from `add` and writes no profile: a package
// manager reporting success over a run that produced nothing. Whether any real
// bundle behaves this way, the daemon must not disagree with
// dshMulticaProfilePresent about whether a profile exists, because that is the
// fact every other part of it reads.
func fakeDshSilentlySucceeds(t *testing.T) (path, record string) {
	t.Helper()
	return fakeDshScript(t, ":")
}

func fakeDshScript(t *testing.T, onSuccess string) (path, record string) {
	t.Helper()
	pinnedDshHome(t)
	dir := t.TempDir()
	path = filepath.Join(dir, "dsh")
	record = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		`printf 'args=%s\n' "$*" >> "$DSH_TEST_RECORD"` + "\n" +
		`printf 'path=%s\n' "$PATH" >> "$DSH_TEST_RECORD"` + "\n" +
		"case \"$*\" in\n" +
		"  *FAILME*) printf '%s\\n' 'registry unreachable' >&2; exit 1 ;;\n" +
		"esac\n" +
		onSuccess + "\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_TEST_RECORD", record)
	return path, record
}

// pinnedDshHome points DSH_HOME at a directory the test owns and reports it.
//
// Without this, dshMulticaProfilePresent() reads the developer's real ~/.dsh,
// so whether a test passes depends on whether the machine running it happens to
// have a `multica` profile installed. That is the ambient-agent-state
// dependency the repo forbids, and it hid here until the install path started
// checking the profile rather than only the exit status.
func pinnedDshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	return home
}

// installMulticaProfile creates the manifest dshMulticaProfilePresent looks for.
func installMulticaProfile(t *testing.T, dshHome string) {
	t.Helper()
	dir := filepath.Join(dshHome, "profiles", dshMulticaProfileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRecord(t *testing.T, record string) string {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// An unconfigured daemon must leave the user's DSH installation alone: the
// reported drop is the whole response.
func TestProvisionDshMulticaProfile_UnconfiguredIsANoOp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, record := fakeDsh(t)
	t.Setenv(dshProfileBundleEnv, "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := provisionDshMulticaProfile(context.Background(), dshPath, nil, logger); err != nil {
		t.Fatalf("provision returned %v, want nil", err)
	}
	if got := readRecord(t, record); got != "" {
		t.Fatalf("dsh was invoked despite no configured bundle: %q", got)
	}
}

func TestProvisionDshMulticaProfile_InstallsTheFirstWorkingCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, record := fakeDsh(t)
	pluginDir := t.TempDir()
	t.Setenv(dshProfileBundleEnv, " FAILME-package , @multica-ai/dsh-runtime , /tmp/local-bundle ")
	t.Setenv(dshPluginPathEnv, pluginDir)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := provisionDshMulticaProfile(context.Background(), dshPath, nil, logger); err != nil {
		t.Fatalf("provision returned %v, want nil once a later candidate succeeds", err)
	}

	got := readRecord(t, record)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("dsh was invoked %d time(s), want 2 (first candidate fails, second succeeds):\n%s", len(lines)/2, got)
	}
	// Candidates are trimmed and tried in order.
	first := "args=plugin --profile multica add FAILME-package"
	if lines[0] != first {
		t.Errorf("first invocation = %q, want %q", lines[0], first)
	}
	second := "args=plugin --profile multica add @multica-ai/dsh-runtime"
	if lines[2] != second {
		t.Errorf("second invocation = %q, want %q", lines[2], second)
	}
	// The third candidate must not run once one has succeeded.
	if strings.Contains(got, "local-bundle") {
		t.Errorf("a candidate was tried after one succeeded:\n%s", got)
	}
	// DSH Desktop ships pnpm, and the daemon inherits no path to it; without
	// this the install fails with "pnpm not found on PATH".
	for _, line := range []string{lines[1], lines[3]} {
		if !strings.HasPrefix(line, "path="+pluginDir+string(os.PathListSeparator)) {
			t.Errorf("PATH = %q, want %q prepended", line, pluginDir)
		}
	}
}

// The whole point of the knob is to be able to point it at a local build before
// Multica's bridge is published (multica#6936), so a directory spec must reach
// dsh unmodified.
func TestProvisionDshMulticaProfile_ForwardsTheSpecVerbatim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, record := fakeDsh(t)
	t.Setenv(dshProfileBundleEnv, "/Users/someone/builds/dsh-multica-runtime")
	t.Setenv(dshPluginPathEnv, "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := provisionDshMulticaProfile(context.Background(), dshPath, nil, logger); err != nil {
		t.Fatalf("provision returned %v, want nil", err)
	}
	want := "args=plugin --profile multica add /Users/someone/builds/dsh-multica-runtime"
	if got := readRecord(t, record); !strings.Contains(got, want) {
		t.Fatalf("recorded %q, want it to contain %q", got, want)
	}
}

func TestProvisionDshMulticaProfile_ReportsEveryCandidateFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, _ := fakeDsh(t)
	t.Setenv(dshProfileBundleEnv, "FAILME-one,FAILME-two")
	t.Setenv(dshPluginPathEnv, "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := provisionDshMulticaProfile(context.Background(), dshPath, nil, logger)
	if err == nil {
		t.Fatal("provision returned nil, want the last failure")
	}
	// The CLI's own output is the only place "pnpm not found on PATH" and
	// registry errors surface, so it has to ride along — and the candidate is
	// named by position, because the spec itself can be an npm URL with
	// credentials in its userinfo or a path carrying a username.
	if !strings.Contains(err.Error(), "candidate 2 of 2") || !strings.Contains(err.Error(), "registry unreachable") {
		t.Fatalf("error = %q, want the failing candidate position and dsh's output", err)
	}
	if strings.Contains(err.Error(), "FAILME-two") {
		t.Fatalf("the bundle spec leaked into the error: %q", err)
	}
}

// The package manager's exit status is not the fact that matters. A candidate
// that returns 0 and writes no profile leaves the operator exactly where a
// failure does, so it must not end the loop, must not be logged as an install,
// and must let the next candidate try.
func TestProvisionDshMulticaProfile_ExitZeroWithoutAProfileIsAFailedCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, record := fakeDshSilentlySucceeds(t)
	t.Setenv(dshProfileBundleEnv, "quiet-one,quiet-two")
	t.Setenv(dshPluginPathEnv, "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := provisionDshMulticaProfile(context.Background(), dshPath, nil, logger)
	if err == nil {
		t.Fatal("provision returned nil while the profile is still absent")
	}
	if !strings.Contains(err.Error(), "without creating the profile") {
		t.Errorf("error = %q, want it to name the empty success", err)
	}
	// Both candidates get a turn: the first one's exit status bought it nothing.
	got := readRecord(t, record)
	for _, want := range []string{"add quiet-one", "add quiet-two"} {
		if !strings.Contains(got, want) {
			t.Errorf("candidate %q was never tried:\n%s", want, got)
		}
	}
}

// Once per daemon, not once per discovery round: this is a network install that
// writes into the user's DSH home, and the caller uses the return value to tell
// the user which of the two situations they are in.
func TestStartDshProfileProvision_RunsAtMostOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, record := fakeDsh(t)
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
	t.Setenv(dshPluginPathEnv, "")

	// A tracked workspace keeps the finished install on the cheap kick path;
	// the zero-workspace bootstrap it would otherwise take runs a full
	// workspace sync and has its own test.
	d := &Daemon{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces: map[string]*workspaceState{"ws-1": {}},
	}
	if !d.startDshProfileProvision(dshPath, nil) {
		t.Fatal("first call did not start the install")
	}
	if d.startDshProfileProvision(dshPath, nil) {
		t.Fatal("second call started another install; the guard is per-round instead of per-daemon")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readRecord(t, record), "plugin --profile multica add") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the install never ran; recorded %q", readRecord(t, record))
}

func TestStartDshProfileProvision_UnconfiguredNeverStarts(t *testing.T) {
	t.Setenv(dshProfileBundleEnv, "")
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if d.startDshProfileProvision("/nonexistent/dsh", nil) {
		t.Fatal("provision started with no configured bundle, which would mutate the user's DSH install unasked")
	}
}

// A finished install has to ask for an immediate round. The discovery loop
// backs off while a provider cannot register, and dsh counts as "missing a
// runtime" for as long as the profile is absent — so the scheduled attempt this
// replaces can be agentConvergeMaxBackoff (30m) away, which reads as "the
// install did nothing" to anyone watching the runtime list.
//
// What counts as "finished" is the profile on disk, not the exit status: the
// fixture writes the manifest, because an install that returns 0 without
// producing one leaves the operator exactly where a failure does.
func TestStartDshProfileProvision_KicksDiscoveryWhenTheInstallFinishes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, _ := fakeDsh(t)
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
	t.Setenv(dshPluginPathEnv, "")

	d := &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentDiscoveryKick: make(chan struct{}, 1),
		// A tracked workspace, so the finished install takes the cheap kick
		// rather than the workspace-sync path a zero-workspace bootstrap needs
		// (registerAfterDshProfileInstall). That path has its own test.
		workspaces: map[string]*workspaceState{"ws-1": {}},
	}
	if !d.startDshProfileProvision(dshPath, nil) {
		t.Fatal("the install did not start")
	}
	select {
	case <-d.agentDiscoveryKick:
	case <-time.After(10 * time.Second):
		t.Fatal("the finished install never asked for a discovery round")
	}
}

// The mirror image, and the reason the check is the manifest rather than the
// exit status: an install that reports success but writes no profile must not
// buy a discovery round — the round would re-probe, find the profile still
// missing, and drop dsh again — and it must withdraw the "installing" claim it
// left on any runtime it took offline (MUL-6164).
func TestStartDshProfileProvision_SuccessWithoutAProfileWithdrawsTheWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dshPath, _ := fakeDshSilentlySucceeds(t)
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
	t.Setenv(dshPluginPathEnv, "")

	rec, client := newDeregisterRecorder(t)
	d := &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:             client,
		workspaces:         map[string]*workspaceState{"ws-1": {}},
		runtimeIndex:       map[string]Runtime{},
		agentDiscoveryKick: make(chan struct{}, 1),
	}
	d.dshInstallWaits = []dshInstallWait{{
		workspaceID: "ws-1",
		runtimeID:   "rt-dsh",
		reason: RuntimeOfflineReason{
			Code:       RuntimeOfflineCodeDshProfile,
			Detail:     dshProfileInstallStartedReason,
			Installing: true,
		},
	}}

	if !d.startDshProfileProvision(dshPath, nil) {
		t.Fatal("the install did not start")
	}

	deadline := time.After(10 * time.Second)
	for {
		if _, ok := rec.reasonFor("rt-dsh"); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the wait was never withdrawn; the runtime queues work behind an install that gave up")
		case <-time.After(20 * time.Millisecond):
		}
	}
	got, _ := rec.reasonFor("rt-dsh")
	if got.Installing {
		t.Error("the corrected reason still claims an install is running")
	}
	select {
	case <-d.agentDiscoveryKick:
		t.Error("an install that produced no profile asked for a discovery round anyway")
	default:
	}
}

// A producer must never wait on the discovery loop, and a burst has to collapse
// into one round rather than a queue of them.
func TestKickAgentDiscovery_NonBlockingAndCollapsing(t *testing.T) {
	d := &Daemon{agentDiscoveryKick: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			d.kickAgentDiscovery()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kickAgentDiscovery blocked; the producer must not wait on the loop")
	}
	if got := len(d.agentDiscoveryKick); got != 1 {
		t.Fatalf("queued kicks = %d, want 1", got)
	}
	// A zero-value Daemon (tests build one directly) has no channel and must
	// not panic or block.
	(&Daemon{}).kickAgentDiscovery()
}

// The stat is what the discovery loop polls every tick to decide whether to
// force a round, so it has to mean exactly what DSH means by "this profile is
// installed" — a directory alone is not one.
func TestDshMulticaProfilePresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)

	if dshMulticaProfilePresent() {
		t.Fatal("profile reported installed before it exists")
	}
	dir := filepath.Join(home, "profiles", dshMulticaProfileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// DSH's loadProfile falls back to a built-in template (or fails, for a
	// profile that has none) when the manifest is missing, so a bare directory
	// is not an installed profile.
	if dshMulticaProfilePresent() {
		t.Fatal("a directory without a manifest was reported as an installed profile")
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !dshMulticaProfilePresent() {
		t.Fatal("manifest present but the profile was reported missing")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if dshMulticaProfilePresent() {
		t.Fatal("removing the profile was not observed; the loop would never force a demotion round")
	}
}

// Windows spells the variable `Path`, and Go de-duplicates the child
// environment case-insensitively: a merge that only matched `PATH=` would leave
// two entries, keep the appended one, and hand the package manager a PATH of
// nothing but the plugin directory.
func TestMergeDshPluginPath(t *testing.T) {
	dirs := []string{"/plugin/dir"}
	sep := string(os.PathListSeparator)

	for _, key := range []string{"PATH", "Path", "path"} {
		t.Run(key, func(t *testing.T) {
			env := mergeDshPluginPath([]string{key + "=/system/bin"}, dirs)
			if len(env) != 1 {
				t.Fatalf("env = %q, want the existing entry merged in place rather than a second one", env)
			}
			want := key + "=" + "/plugin/dir" + sep + "/system/bin"
			if env[0] != want {
				t.Fatalf("env[0] = %q, want %q (spelling preserved, system path kept)", env[0], want)
			}
		})
	}

	t.Run("no path entry at all", func(t *testing.T) {
		env := mergeDshPluginPath([]string{"HOME=/home/x"}, dirs)
		if len(env) != 2 || env[1] != "PATH=/plugin/dir" {
			t.Fatalf("env = %q, want an appended PATH", env)
		}
	})

	t.Run("nothing to prepend leaves the env alone", func(t *testing.T) {
		env := mergeDshPluginPath([]string{"PATH=/system/bin"}, nil)
		if len(env) != 1 || env[0] != "PATH=/system/bin" {
			t.Fatalf("env = %q, want it untouched", env)
		}
	})
}

// The argv and the environment the install runs under are the contract with the
// package manager, so they are assertable without one — which is also the only
// coverage Windows can get, since the shell fixtures cannot stand in for pnpm.
func TestDshProvisionCommand(t *testing.T) {
	pluginDir := t.TempDir()
	t.Setenv(dshPluginPathEnv, pluginDir)

	cmd := dshProvisionCommand("/usr/local/bin/dsh", "@multica-ai/dsh-runtime", nil)
	want := []string{"/usr/local/bin/dsh", "plugin", "--profile", "multica", "add", "@multica-ai/dsh-runtime"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q, want %q", cmd.Args, want)
	}
	if cmd.Env == nil {
		t.Fatal("Env is nil: the install would inherit nothing, not even the plugin directory")
	}
}

func TestDshProvisionOutput(t *testing.T) {
	t.Run("bounded from the tail", func(t *testing.T) {
		long := strings.Repeat("x", dshProvisionOutputBytes*2) + "THE-END"
		got := dshProvisionOutput([]byte(long))
		if len(got) > dshProvisionOutputBytes+len("…") {
			t.Fatalf("len = %d, want at most %d", len(got), dshProvisionOutputBytes+1)
		}
		if !strings.HasSuffix(got, "THE-END") || !strings.HasPrefix(got, "…") {
			t.Fatalf("got %q, want a truncated tail", got[:min(40, len(got))])
		}
	})

	// Both shapes a package manager leaks a credential in: the .npmrc-style
	// token line, which the shared redactor knows, and the request URL itself —
	// which is the one the bundle spec turns into when it is an authenticated
	// npm URL, and which is scrubbed here rather than in pkg/redact, because a
	// rule added there changes output for every provider.
	t.Run("redacts credentials a registry echoed back", func(t *testing.T) {
		for _, tc := range []struct{ raw, secret string }{
			{"npm ERR! 401 //registry.example/:_authToken=supersecret123", "supersecret123"},
			{"npm ERR! 404 https://deploy:hunter2@registry.example.com/@acme/dsh-runtime", "hunter2"},
			{"ERR_PNPM_FETCH_401 GET http://ci-bot:s3cr3t@npm.internal/@corp%2fdsh", "s3cr3t"},
		} {
			got := dshProvisionOutput([]byte(tc.raw))
			if strings.Contains(got, tc.secret) {
				t.Errorf("output = %q, want %q redacted", got, tc.secret)
			}
		}
	})

	// The host has to survive: which registry refused the request is what an
	// operator reads, and it is not the secret.
	t.Run("keeps the host", func(t *testing.T) {
		got := dshProvisionOutput([]byte("npm ERR! 404 https://deploy:hunter2@registry.example.com/pkg"))
		if !strings.Contains(got, "registry.example.com") {
			t.Errorf("output = %q, want the host kept", got)
		}
	})

	// Userinfo needs both a colon and an @ before the next path separator. A
	// port, an scp-style git remote and a bare timestamp must not trip it.
	t.Run("leaves ordinary URLs intact", func(t *testing.T) {
		for _, raw := range []string{
			"GET https://registry.example.com:8443/@acme/pkg failed",
			"cloning ssh://git@github.com/multica-ai/multica.git",
			"see https://example.com/a:b for details",
		} {
			if got := dshProvisionOutput([]byte(raw)); got != raw {
				t.Errorf("dshProvisionOutput(%q) = %q, want it unchanged", raw, got)
			}
		}
	})
}

// processAlive reports whether a pid still refers to a running process.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Cancelling the daemon must stop the install's whole process tree. `dsh plugin`
// forwards to pnpm, which spawns node, which runs the bundle's install script —
// and the direct child is only the first of those. Killing just it leaves the
// rest running, still able to write into the operator's DSH home after the
// daemon is gone, and a daemon rollback cannot undo a write that already landed.
//
// The fixture hands the grandchild's pid back before the parent settles into its
// own wait, so the assertion is about survival rather than about a race: an
// unsynchronized grandchild can be killed before it publishes anything, which
// would let this pass without testing anything.
func TestProvisionDshMulticaProfile_CancellationKillsTheWholeTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture; the tree is a Job Object there")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "late-write.log")
	pidFile := filepath.Join(dir, "grandchild.pid")
	dshPath := filepath.Join(dir, "dsh")
	// The grandchild publishes its pid, then writes the record well after the
	// cancellation below — so the file is evidence of a survivor, and the pid is
	// evidence of one that never needed to write at all.
	script := "#!/bin/sh\n" +
		"sh -c 'printf %s $$ > " + pidFile + "; sleep 2; printf late >> " + record + "' >/dev/null 2>&1 &\n" +
		"while [ ! -s " + pidFile + " ]; do sleep 0.05; done\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(dshPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provisionDone := make(chan error, 1)
	go func() {
		provisionDone <- provisionDshMulticaProfile(ctx, dshPath, nil,
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			if parsed, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && parsed > 0 {
				pid = parsed
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the fixture never published its grandchild pid; the test cannot judge survival")
	}
	if !processAlive(pid) {
		t.Fatalf("grandchild %d was already gone before cancellation", pid)
	}

	cancel()
	if err := <-provisionDone; err == nil {
		t.Fatal("provision returned nil after cancellation")
	}

	// Bounded: processtree interrupts, then stops the tree, then waits for it.
	for i := 0; i < 40 && processAlive(pid); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
		t.Fatalf("grandchild %d survived cancellation; it could still write to the DSH home", pid)
	}
	if data, err := os.ReadFile(record); err == nil {
		t.Fatalf("a grandchild outlived cancellation and wrote: %s", data)
	}
}
