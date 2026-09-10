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
)

func TestProbeDshMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "compatible", body: `printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","plugin_version":"test","protocol_version":1}'`, want: true},
		{name: "missing plugin", body: `printf '%s\n' 'profile not installed'`, want: false},
		{name: "future protocol", body: `printf '%s\n' '{"v":2,"type":"probe","runtime":"dsh","protocol_version":2}'`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dsh")
			script := "#!/bin/sh\nset -eu\n" + tc.body + "\n"
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := probeDshMulticaProfile(path); got != tc.want {
				t.Fatalf("probeDshMulticaProfile() = %v, want %v", got, tc.want)
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
func TestProbeBuiltinRuntime_DshWithoutMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tests := []struct {
		name        string
		probeBody   string
		wantVerdict builtinProbeVerdict
	}{
		{
			name:        "profile installed",
			probeBody:   `printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","plugin_version":"test","protocol_version":1}'`,
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
			path := filepath.Join(t.TempDir(), "dsh")
			// --probe answers per the case; every other invocation (the
			// version detection that runs on the OK path) reports a version.
			script := "#!/bin/sh\nset -eu\ncase \"$*\" in\n  *--probe*) " + tc.probeBody + " ;;\n  *) printf '%s\\n' '0.1.2-rc.1' ;;\nesac\n"
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

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
