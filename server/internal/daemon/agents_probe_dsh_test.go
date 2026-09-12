package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dshProbeFixture writes a fake dsh and controls the one filesystem fact the
// classification turns on: whether the `multica` profile manifest is present.
// DSH_HOME is pinned so the answer never depends on the machine running the
// test.
//
// body answers `--probe`; every other invocation reports a version. That split
// is the premise of the whole feature, not fixture decoration: a dsh whose
// profile is missing runs, resolves, and answers `--version` perfectly well —
// being unable to answer `--probe` is the ONLY thing wrong with it. A fixture
// that failed both would be a broken CLI, which the daemon now classifies as a
// failed version probe and never as a profile verdict.
func dshProbeFixture(t *testing.T, body string, manifest bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	if manifest {
		dir := filepath.Join(home, "profiles", dshMulticaProfileName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "dsh")
	script := "#!/bin/sh\ncase \"$*\" in\n  *--probe*) " + body + " ;;\n  *) printf '%s\\n' '0.1.2-rc.1' ;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const dshProbeOKFrame = `printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","plugin_version":"test","protocol_version":1}'`

// Only a profile that is genuinely absent may be installed or condemned. Every
// other failure mode is a statement about this instant, and treating them alike
// is what let a momentary failure during a DSH upgrade demote a working runtime
// and start overwriting an installation that was already there.
func TestProbeDshMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tests := []struct {
		name     string
		body     string
		manifest bool
		want     dshProbeVerdict
	}{
		{
			name: "compatible", body: dshProbeOKFrame, manifest: true, want: dshProbeOK,
		},
		{
			name: "profile absent and the probe says so",
			body: `printf '%s\n' 'profile "multica" does not exist' >&2; exit 1`,
			want: dshProbeMissingProfile,
		},
		{
			name: "profile absent and the probe fails opaquely",
			body: `printf '%s\n' 'boom' >&2; exit 3`, want: dshProbeMissingProfile,
		},
		{
			name:     "manifest present but the probe exits non-zero",
			body:     `printf '%s\n' 'cannot boot' >&2; exit 1`,
			manifest: true, want: dshProbeUnavailable,
		},
		{
			name: "manifest present but the probe times out",
			body: `exec sleep 30`, manifest: true, want: dshProbeUnavailable,
		},
		{
			// The case the whole classification turns on. Asked for a profile
			// it does not have, DSH does not refuse — it hangs, printing
			// nothing. Calling that transient made a missing profile
			// undetectable on the one host the automatic install exists for.
			name: "probe hangs with no manifest",
			body: `exec sleep 30`, want: dshProbeMissingProfile,
		},
		{
			name: "exit 0 printing nothing, no manifest",
			body: `exit 0`, want: dshProbeMissingProfile,
		},
		{
			// A frame is an answer even when the process exits badly
			// afterwards: the profile spoke the protocol.
			name:     "valid frame followed by a non-zero exit",
			body:     dshProbeOKFrame + `; exit 3`,
			manifest: true, want: dshProbeOK,
		},
		{
			name:     "manifest present but the output is unparseable",
			body:     `printf '%s\n' 'not json at all'`,
			manifest: true, want: dshProbeUnavailable,
		},
		{
			name: "exit 0 printing nothing",
			body: `exit 0`, manifest: true, want: dshProbeUnavailable,
		},
		{
			name:     "manifset present and the protocol is newer",
			body:     `printf '%s\n' '{"v":2,"type":"probe","runtime":"dsh","protocol_version":2}'`,
			manifest: true, want: dshProbeIncompatible,
		},
		{
			name:     "manifest present and the runtime is something else",
			body:     `printf '%s\n' '{"v":1,"type":"probe","runtime":"other","protocol_version":1}'`,
			manifest: true, want: dshProbeIncompatible,
		},
	}
	origTimeout := dshProbeTimeout
	t.Cleanup(func() { dshProbeTimeout = origTimeout })
	dshProbeTimeout = 2 * time.Second

	// A cancelled round says nothing about the profile, whatever the manifest
	// says: the probe never got to finish on its own terms.
	t.Run("a cancelled round stays transient", func(t *testing.T) {
		path := dshProbeFixture(t, `exec sleep 30`, false)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got := probeDshMulticaProfile(ctx, path, nil); got != dshProbeUnavailable {
			t.Fatalf("probeDshMulticaProfile() = %v, want %v", got, dshProbeUnavailable)
		}
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := dshProbeFixture(t, tc.body, tc.manifest)
			if got := probeDshMulticaProfile(context.Background(), path, nil); got != tc.want {
				t.Fatalf("probeDshMulticaProfile() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The four ways a probe can fail while the profile IS installed must not
// condemn the runtime and must not install anything over it. Each previously
// collapsed into "missing profile", which demoted immediately and, with a
// bundle configured, started overwriting the operator's installation.
//
// "Not condemned" is asserted through demotableBuiltinProbeVerdict rather than
// by naming the verdicts this must not be: the property under test is that no
// route from here reaches a demotion, and a verdict added to the demotable set
// later has to fail this test rather than slip past a list of names.
func TestProbeBuiltinRuntime_DshFailureWithProfileInstalledIsNotCondemned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "timeout", body: `exec sleep 30`},
		{name: "non-zero exit", body: `printf '%s\n' 'cannot boot' >&2; exit 1`},
		{name: "malformed output", body: `printf '%s\n' 'not json at all'`},
		{name: "future protocol", body: `printf '%s\n' '{"v":2,"type":"probe","runtime":"dsh","protocol_version":2}'`},
	}
	origTimeout, origDelay, origWindow := dshProbeTimeout, runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() {
		dshProbeTimeout, runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow = origTimeout, origDelay, origWindow
	})
	dshProbeTimeout = 300 * time.Millisecond
	runtimeVersionProbeRetryDelay = time.Millisecond
	runtimeVersionProbeRetryWindow = 10 * time.Millisecond

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := dshProbeFixture(t, tc.body, true)
			record := filepath.Join(t.TempDir(), "provisioned.log")
			// A bundle IS configured: the assertion is that these failures never
			// reach the install path.
			t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
			t.Setenv("DSH_TEST_RECORD", record)

			d := &Daemon{
				logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
				agentVersions: map[string]string{},
			}
			_, reason, verdict := d.probeBuiltinRuntime(context.Background(), "dsh",
				AgentEntry{Path: path, Command: "dsh"})

			if verdict == builtinProbeMissingProfile {
				t.Fatalf("verdict = missing profile (reason %q); a failing probe over an installed profile must not be installable", reason)
			}
			if verdict == builtinProbeOK {
				t.Fatalf("verdict = OK for a probe that produced no usable answer")
			}
			if demotableBuiltinProbeVerdict(verdict) {
				t.Fatalf("verdict %v (reason %q) can take a live runtime offline; "+
					"an installed profile behind one failed probe is not evidence for that", verdict, reason)
			}
			// The install runs in a goroutine; give it a chance to be wrong.
			time.Sleep(200 * time.Millisecond)
			if data, err := os.ReadFile(record); err == nil {
				t.Fatalf("the provisioning path ran against an installed profile: %s", data)
			}
		})
	}
}

// Discovery is deliberately profile-blind: probeAgentCLIs answers "is there a
// dsh binary here", and the usability gate lives one layer up in
// probeBuiltinRuntime, where a drop produces a verdict the user can see.
//
// Gating discovery on the profile instead — which this test used to assert —
// is what made a missing profile invisible: dsh vanished from the availability
// set with nothing on /health, in the log, or in `daemon status` to say why.
func TestProbeAgentCLIsDiscoversDshWithoutMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	originalResolver := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = originalResolver })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	fakeDir := t.TempDir()
	path := filepath.Join(fakeDir, "dsh")
	body := "#!/bin/sh\nset -eu\nprintf '%s\\n' 'missing multica profile' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir)
	t.Setenv("MULTICA_DSH_PATH", "")

	entry, found := probeAgentCLIs()["dsh"]
	if !found {
		t.Fatal("dsh was not discovered; discovery must not depend on the Multica runtime profile")
	}
	if want := canonicalExecutablePath(path); entry.Path != want {
		t.Fatalf("dsh path = %q, want %q", entry.Path, want)
	}
}

// The gate that discovery gave up lives here now, and its verdict is what makes
// the drop visible. A dsh whose profile is missing resolves and answers
// `--version` like any healthy CLI, so without this verdict the daemon would
// register a runtime that fails every task it is handed.
//
// The manifest is what decides, so the fixture owns it: a probe that fails over
// an installed profile is not the same finding as one that fails with nothing
// installed, and only the second may be installed over.
func TestProbeBuiltinRuntime_DshWithoutMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tests := []struct {
		name        string
		probeBody   string
		manifest    bool
		wantVerdict builtinProbeVerdict
	}{
		{
			name:        "profile installed",
			probeBody:   dshProbeOKFrame,
			manifest:    true,
			wantVerdict: builtinProbeOK,
		},
		{
			name:        "profile missing",
			probeBody:   `printf '%s\n' 'profile "multica" does not exist' >&2; exit 1`,
			wantVerdict: builtinProbeMissingProfile,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := dshProbeFixture(t, tc.probeBody, tc.manifest)

			d := &Daemon{
				logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
				agentVersions: map[string]string{},
			}
			_, reason, verdict := d.probeBuiltinRuntime(
				context.Background(), "dsh", AgentEntry{Path: path, Command: "dsh"})
			if verdict != tc.wantVerdict {
				t.Fatalf("verdict = %v, want %v (reason %q)", verdict, tc.wantVerdict, reason)
			}
			if tc.wantVerdict == builtinProbeMissingProfile && !strings.Contains(reason, "profile") {
				t.Fatalf("reason = %q, want it to name the missing runtime profile", reason)
			}
		})
	}
}

// A binary that is not there must not be reported as a missing profile. The
// probe cannot tell "the profile refused" from "there was nothing to run", so
// without the executable check the reason sends the user to install a bundle
// that is not the problem, while the path that actually vanished goes
// unmentioned.
func TestProbeBuiltinRuntime_DshBinaryGoneIsNotAProfileVerdict(t *testing.T) {
	d := &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentVersions: map[string]string{},
	}
	gone := filepath.Join(t.TempDir(), "dsh-that-was-removed")
	_, reason, verdict := d.probeBuiltinRuntime(context.Background(), "dsh",
		AgentEntry{Path: gone, Command: ""})
	if verdict == builtinProbeMissingProfile {
		t.Fatalf("verdict = missing profile (reason %q), want the version-detection path to report the vanished binary", reason)
	}
}

// dshDesktopBundleFixture writes a fake DSH Desktop bundled CLI that answers
// `--probe` the way the Multica runtime profile does, and pins discovery to
// it. The DSH Desktop app ships its CLI inside the .app bundle and never
// installs `dsh` onto PATH, so neither LookPath nor the login-shell sweep can
// find it.
func dshDesktopBundleFixture(t *testing.T, mode os.FileMode) string {
	t.Helper()
	originalResolver := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = originalResolver })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	bundle := filepath.Join(t.TempDir(), "DSH Desktop.app", "Contents", "Resources",
		"app.asar.unpacked", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\n" + `printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","plugin_version":"test","protocol_version":1}'` + "\n"
	if err := os.WriteFile(bundle, []byte(script), mode); err != nil {
		t.Fatal(err)
	}

	originalBundles := dshDesktopAppBundlePaths
	dshDesktopAppBundlePaths = func() []string { return []string{bundle} }
	t.Cleanup(func() { dshDesktopAppBundlePaths = originalBundles })

	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_DSH_PATH", "")
	t.Setenv("MULTICA_DSH_MODEL", "deepseek-official/deepseek-chat")
	return bundle
}

func TestProbeAgentCLIsUsesDshDesktopAppBundleFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the DSH Desktop app bundle fallback is macOS-only")
	}
	bundle := dshDesktopBundleFixture(t, 0o755)

	entry, found := probeAgentCLIs()["dsh"]
	if !found {
		t.Fatal("dsh was not discovered from the DSH Desktop app bundle")
	}
	if entry.Path != bundle {
		t.Fatalf("dsh path = %q, want %q", entry.Path, bundle)
	}
	if entry.Command != "dsh" {
		t.Fatalf("dsh command = %q, want dsh", entry.Command)
	}
	if entry.Model != "deepseek-official/deepseek-chat" {
		t.Fatalf("dsh model = %q, want the MULTICA_DSH_MODEL override", entry.Model)
	}
}

// A bundled CLI that exists but cannot be spawned must stay unregistered:
// registering it would advertise a healthy runtime whose every task fails.
func TestProbeAgentCLIsIgnoresNonExecutableDshBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the DSH Desktop app bundle fallback is macOS-only")
	}
	dshDesktopBundleFixture(t, 0o644)

	if _, found := probeAgentCLIs()["dsh"]; found {
		t.Fatal("dsh was registered from a non-executable app bundle path")
	}
}

// The Windows incident, as a test: DSH Desktop's CLI is a .cmd shim that
// exec.LookPath resolves happily and that exits 9009 — cmd.exe's "command not
// found" — because what it forwards to is missing. Every invocation fails,
// `--version` included.
//
// Probing the profile before version detection reported that machine as "the
// Multica runtime profile is not installed": a repair for a problem it did not
// have, sending the user to install a bundle while the actual fault was a CLI
// that cannot execute — and, with a bundle configured, starting that install
// against it. A CLI that cannot answer `--version` is not one this daemon has
// any business holding an opinion about the profile of.
func TestProbeBuiltinRuntime_DshThatCannotRunIsNotAProfileVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	origDelay, origWindow := runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() {
		runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow = origDelay, origWindow
	})
	runtimeVersionProbeRetryDelay = time.Millisecond
	runtimeVersionProbeRetryWindow = 10 * time.Millisecond

	// No DSH_HOME profile and a CLI that fails whatever it is asked — the exact
	// shape that used to be classified as a missing profile.
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	path := filepath.Join(t.TempDir(), "dsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 9009\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "provisioned.log")
	// A bundle IS configured: the assertion is that an unrunnable CLI never
	// reaches the install path.
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
	t.Setenv("DSH_TEST_RECORD", record)

	d := &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentVersions: map[string]string{},
	}
	_, reason, verdict := d.probeBuiltinRuntime(context.Background(), "dsh",
		AgentEntry{Path: path, Command: "dsh"})

	if verdict == builtinProbeMissingProfile {
		t.Fatalf("verdict = missing profile (reason %q); the CLI cannot run at all, "+
			"and installing a bundle into it is not the repair", reason)
	}
	if demotableBuiltinProbeVerdict(verdict) {
		t.Fatalf("verdict %v (reason %q) can take a live runtime offline on one failed exec", verdict, reason)
	}
	// The reason has to name the real fault, or the user goes looking for a
	// profile problem that does not exist.
	if !strings.Contains(reason, "version detection failed") {
		t.Errorf("reason = %q, want it to name the failed version probe", reason)
	}
	time.Sleep(200 * time.Millisecond)
	if data, err := os.ReadFile(record); err == nil {
		t.Fatalf("the provisioning path ran against a CLI that cannot execute: %s", data)
	}
}

// A profile verdict has to reach the server as a structured cause, not as a
// bare "offline": without it the server classifies the runtime as merely
// offline, keeps queueing assignments behind a wait that only a human can end,
// and has no repair to show. The installing flag is the other half — it is what
// tells the server this particular wait does resolve itself.
func TestNewRuntimeVerdict_DshProfileCarriesAStructuredCause(t *testing.T) {
	for _, tc := range []struct {
		name       string
		verdict    builtinProbeVerdict
		installing bool
	}{
		{name: "missing, no install", verdict: builtinProbeMissingProfile},
		{name: "missing, install running", verdict: builtinProbeMissingProfile, installing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := newRuntimeVerdict(tc.verdict, "explained elsewhere", "/usr/local/bin/dsh", tc.installing)
			if got.offline == nil {
				t.Fatal("offline reason is nil: the server would treat this as a machine that is merely offline")
			}
			if got.offline.Code != RuntimeOfflineCodeDshProfile {
				t.Fatalf("code = %q, want %q", got.offline.Code, RuntimeOfflineCodeDshProfile)
			}
			if got.offline.Installing != tc.installing {
				t.Fatalf("installing = %v, want %v", got.offline.Installing, tc.installing)
			}
			// A repair that NAMES what is missing, and deliberately carries no
			// command: the install needs a bundle only the operator can
			// choose, and Repair.Command is rendered to the user inside a
			// shell fence as the line to run, so a placeholder there is a
			// paste-and-fail instruction.
			if got.offline.Repair == nil || got.offline.Repair.Package == "" {
				t.Fatalf("repair = %+v, want the missing package named", got.offline.Repair)
			}
			if strings.Contains(got.offline.Repair.Command, "<") {
				t.Fatalf("repair command %q hands the user a placeholder to run", got.offline.Repair.Command)
			}
		})
	}

	// An incompatible profile never becomes an offline reason at all: it is not
	// demotable, so nothing deregisters a runtime for it.
	if demotableBuiltinProbeVerdict(builtinProbeIncompatibleProfile) {
		t.Fatal("an incompatible profile can demote a live runtime; " +
			"the daemon may be the stale side of that protocol skew")
	}
}

func TestProbeAgentCLIsMiseManagedDshUsesPairedEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}

	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	trustedBin := filepath.Join(root, "trusted-bin")
	hostileBin := filepath.Join(root, "hostile-bin")
	for _, dir := range []string{shimDir, trustedBin, hostileBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	target := filepath.Join(root, "dsh-target")
	if err := os.WriteFile(target, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trustedBin, "node"), []byte("#!/bin/sh\ncase \"$*\" in *--version*) printf '%s\\n' '0.1.2-rc.1'; exit 0 ;; esac\nprintf '%s\\n' '{\"v\":1,\"type\":\"probe\",\"runtime\":\"dsh\",\"protocol_version\":1}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostileBin, "node"), []byte("#!/bin/sh\nexit 91\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	manager := filepath.Join(root, "mise")
	managerBody := "#!/bin/sh\nset -eu\ncase \"$1\" in\n  which) printf '%s\\n' \"$TEST_DSH_TARGET\" ;;\n  env) printf '{\"PATH\":\"%s:/usr/bin:/bin\"}\\n' \"$TEST_DSH_TRUSTED_BIN\" ;;\n  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(manager, []byte(managerBody), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manager, filepath.Join(shimDir, "dsh")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_DSH_TARGET", target)
	t.Setenv("TEST_DSH_TRUSTED_BIN", trustedBin)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+hostileBin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("MULTICA_DSH_PATH", "")
	t.Setenv("SHELL", filepath.Join(root, "unsupported-shell"))
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv(dshProfileBundleEnv, "")
	resetShellResolveCacheForTest(t)

	entry, ok := probeAgentCLIs()["dsh"]
	if !ok {
		t.Fatal("mise-managed DSH was dropped after its path and environment resolved")
	}
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Path != canonicalTarget || entry.MiseEnv["PATH"] == "" {
		t.Fatalf("dsh entry = %+v, want paired target and environment", entry)
	}
	d := &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentVersions: map[string]string{},
	}
	if version, reason, verdict := d.probeBuiltinRuntime(context.Background(), "dsh", entry); verdict != builtinProbeOK || version == "" {
		t.Fatalf("mise DSH probe = (%q, %q, %v), want a healthy version and profile", version, reason, verdict)
	}
	if got := probeDshMulticaProfile(context.Background(), entry.Path, nil); got == dshProbeOK {
		t.Fatal("unpaired profile probe unexpectedly used the trusted interpreter")
	}
	t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
	t.Setenv(dshPluginPathEnv, hostileBin)
	installer := "#!/bin/sh\nset -eu\ntest \"$TEST_MISE_INSTALL\" = paired\nmkdir -p \"$DSH_HOME/profiles/multica\"\nprintf '{}\\n' > \"$DSH_HOME/profiles/multica/package.json\"\n"
	if err := os.WriteFile(filepath.Join(trustedBin, "node"), []byte(installer), 0o755); err != nil {
		t.Fatal(err)
	}
	entry.MiseEnv["TEST_MISE_INSTALL"] = "paired"
	if err := provisionDshMulticaProfile(context.Background(), entry.Path, nil, d.logger); err == nil {
		t.Fatal("unpaired install unexpectedly succeeded")
	}
	if err := provisionDshMulticaProfile(context.Background(), entry.Path, entry.MiseEnv, d.logger); err != nil {
		t.Fatalf("paired install failed: %v", err)
	}
	if !dshMulticaProfilePresent() {
		t.Fatal("paired install did not create the profile")
	}
}
