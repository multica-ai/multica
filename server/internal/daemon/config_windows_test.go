//go:build windows

package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	longPathExecutableHelperEnv = "MULTICA_LONG_PATH_EXECUTABLE_HELPER"
	longPathExecutableMarkerEnv = "MULTICA_LONG_PATH_EXECUTABLE_MARKER"
)

func TestLongPathExecutableHelper(t *testing.T) {
	if os.Getenv(longPathExecutableHelperEnv) == "1" {
		if marker := os.Getenv(longPathExecutableMarkerEnv); marker != "" {
			executable, err := os.Executable()
			if err != nil {
				os.Exit(2)
			}
			if err := os.WriteFile(marker, []byte(executable), 0o600); err != nil {
				os.Exit(3)
			}
		}
		os.Exit(0)
	}
}

func TestCanonicalExecutablePathResolvesDirectoryJunction(t *testing.T) {
	targetDir := t.TempDir()
	targetBin := filepath.Join(targetDir, "bin")
	if err := os.MkdirAll(targetBin, 0o755); err != nil {
		t.Fatal(err)
	}
	targetExecutable := filepath.Join(targetBin, "codex.exe")
	if err := os.WriteFile(targetExecutable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	shimRoot := t.TempDir()
	shimBin := filepath.Join(shimRoot, "bin")
	createJunction(t, targetBin, shimBin)
	shimExecutable := filepath.Join(shimBin, "codex.exe")

	if _, err := filepath.EvalSymlinks(shimExecutable); err == nil {
		t.Fatal("EvalSymlinks unexpectedly resolved a directory junction; test would not reproduce the bug")
	}
	assertResolvedExecutable(t, canonicalExecutablePath(shimExecutable), shimExecutable, targetExecutable)
}

func TestCanonicalExecutablePathKeepsRegularExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex.exe")
	if err := os.WriteFile(executable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	assertSameFile(t, canonicalExecutablePath(executable), executable)
}

func TestCanonicalExecutablePathFallsBackForMissingExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "codex.exe")
	want, err := filepath.Abs(missing)
	if err != nil {
		t.Fatal(err)
	}

	if got := canonicalExecutablePath(missing); got != want {
		t.Fatalf("canonicalExecutablePath() = %q, want fallback %q", got, want)
	}
}

func TestCanonicalExecutablePathLogsFallbackError(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	missing := filepath.Join(t.TempDir(), "missing", "codex.exe")
	canonicalExecutablePath(missing)

	got := logs.String()
	if !strings.Contains(got, "canonicalize agent executable path failed") {
		t.Fatalf("canonicalization fallback log = %q, want failure message", got)
	}
	if !strings.Contains(got, missing) {
		t.Fatalf("canonicalization fallback log = %q, want path %q", got, missing)
	}
	if !strings.Contains(got, "error=") {
		t.Fatalf("canonicalization fallback log = %q, want error attribute", got)
	}
}

func TestResolveAgentExecutablePathKeepsPATHJunctionEntryPoint(t *testing.T) {
	_, shimBin := stageJunctionExecutable(t)
	t.Setenv("PATH", shimBin)
	t.Setenv("PATHEXT", ".EXE")

	got, err := resolveAgentExecutablePath("codex")
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	if want := filepath.Join(shimBin, "codex.exe"); !strings.EqualFold(got, want) {
		t.Fatalf("resolved PATH entry = %q, want stable junction entry %q", got, want)
	}
}

func TestResolveAgentEntryFollowsRetargetedInstallerJunction(t *testing.T) {
	originalDetect := detectAgentVersion
	detectAgentVersion = func(_ context.Context, runtimeCmd agent.Command) (string, error) {
		path := runtimeCmd.Path
		return filepath.Base(filepath.Dir(filepath.Dir(path))), nil
	}
	t.Cleanup(func() { detectAgentVersion = originalDetect })

	root := t.TempDir()
	releases := filepath.Join(root, "releases")
	installRelease := func(version string) string {
		bin := filepath.Join(releases, version, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		executable := filepath.Join(bin, "codex.exe")
		copyTestExecutable(t, executable)
		return executable
	}
	v1 := installRelease("0.144.1")
	current := filepath.Join(root, "current")
	createJunction(t, filepath.Dir(filepath.Dir(v1)), current)
	visibleBin := filepath.Join(root, "visible", "bin")
	createJunction(t, filepath.Join(current, "bin"), visibleBin)
	stableExecutable := filepath.Join(visibleBin, "codex.exe")

	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: stableExecutable, Command: stableExecutable}
	got, version := d.resolveAgentEntry(context.Background(), "codex", entry)
	assertResolvedExecutable(t, got.Path, stableExecutable, v1)
	if version != "0.144.1" {
		t.Fatalf("initial version = %q, want 0.144.1", version)
	}
	assertLaunchesExecutable(t, got.Path, v1)

	v2 := installRelease("0.144.3")
	if err := os.Remove(current); err != nil {
		t.Fatalf("remove current junction: %v", err)
	}
	createJunction(t, filepath.Dir(filepath.Dir(v2)), current)

	got, version = d.resolveAgentEntry(context.Background(), "codex", entry)
	assertResolvedExecutable(t, got.Path, stableExecutable, v2)
	if version != "0.144.3" {
		t.Fatalf("version after retarget = %q, want 0.144.3", version)
	}
	assertLaunchesExecutable(t, got.Path, v2)
}

func TestResolveAgentEntryForLaunchRejectsUnverifiedInitialJunctionTarget(t *testing.T) {
	tests := []struct {
		name      string
		detect    func(context.Context, agent.Command) (string, error)
		check     func(error) bool
		wantError string
	}{
		{
			name: "version detection failure",
			detect: func(context.Context, agent.Command) (string, error) {
				return "", errors.New("version probe failed")
			},
			check:     func(err error) bool { return strings.Contains(err.Error(), "version probe failed") },
			wantError: "version probe failed",
		},
		{
			name: "below minimum version",
			detect: func(context.Context, agent.Command) (string, error) {
				return "0.9.0", nil
			},
			check: func(err error) bool {
				var tooOld *agent.BelowMinimumError
				return errors.As(err, &tooOld)
			},
			wantError: "BelowMinimumError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDetect := detectAgentVersion
			detectAgentVersion = tt.detect
			t.Cleanup(func() { detectAgentVersion = originalDetect })

			_, shimBin := stageJunctionExecutable(t)
			stableExecutable := filepath.Join(shimBin, "codex.exe")
			d := newSelfHealTestDaemon()
			d.setAgentVersion("codex", "0.144.1")
			entry := AgentEntry{Path: stableExecutable, Command: stableExecutable}

			got, _, err := d.resolveAgentEntryForLaunch(context.Background(), "codex", entry)
			if err == nil {
				t.Fatalf("resolveAgentEntryForLaunch launched unverified target %q; want %s", got.Path, tt.wantError)
			}
			if !tt.check(err) {
				t.Fatalf("resolveAgentEntryForLaunch error = %v, want %s", err, tt.wantError)
			}
		})
	}
}

func TestResolveAgentEntryDoesNotSharePreRetargetSingleflightResult(t *testing.T) {
	root := t.TempDir()
	releases := filepath.Join(root, "releases")
	installRelease := func(version string) string {
		bin := filepath.Join(releases, version, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		executable := filepath.Join(bin, "codex.exe")
		copyTestExecutable(t, executable)
		return executable
	}
	v1 := installRelease("0.144.1")
	v2 := installRelease("0.144.3")
	current := filepath.Join(root, "current")
	createJunction(t, filepath.Dir(filepath.Dir(v1)), current)
	visibleBin := filepath.Join(root, "visible", "bin")
	createJunction(t, filepath.Join(current, "bin"), visibleBin)
	stableExecutable := filepath.Join(visibleBin, "codex.exe")

	v1ProbeStarted := make(chan struct{})
	releaseV1Probe := make(chan struct{})
	v2ProbeStarted := make(chan struct{}, 1)
	originalDetect := detectAgentVersion
	detectAgentVersion = func(_ context.Context, runtimeCmd agent.Command) (string, error) {
		path := runtimeCmd.Path
		switch {
		case sameFile(path, v1):
			select {
			case <-v1ProbeStarted:
			default:
				close(v1ProbeStarted)
			}
			<-releaseV1Probe
			return "0.144.1", nil
		case sameFile(path, v2):
			select {
			case v2ProbeStarted <- struct{}{}:
			default:
			}
			return "0.144.3", nil
		default:
			return "", errors.New("unexpected version probe path")
		}
	}
	t.Cleanup(func() { detectAgentVersion = originalDetect })

	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: stableExecutable, Command: stableExecutable}
	type resolution struct {
		entry   AgentEntry
		version string
	}
	first := make(chan resolution, 1)
	go func() {
		got, version := d.resolveAgentEntry(context.Background(), "codex", entry)
		first <- resolution{entry: got, version: version}
	}()
	<-v1ProbeStarted

	if err := os.Remove(current); err != nil {
		t.Fatalf("remove current junction: %v", err)
	}
	createJunction(t, filepath.Dir(filepath.Dir(v2)), current)

	second := make(chan resolution, 1)
	go func() {
		got, version := d.resolveAgentEntry(context.Background(), "codex", entry)
		second <- resolution{entry: got, version: version}
	}()

	select {
	case <-v2ProbeStarted:
		// The post-retarget caller is verifying v2 independently of the blocked
		// v1 probe, so it cannot inherit the predecessor's singleflight result.
	case <-time.After(5 * time.Second):
		close(releaseV1Probe)
		got := <-second
		t.Fatalf("post-retarget resolution shared the blocked v1 probe: got (%q, %q)", got.entry.Path, got.version)
	}
	close(releaseV1Probe)

	got := <-second
	assertResolvedExecutable(t, got.entry.Path, stableExecutable, v2)
	if got.version != "0.144.3" {
		t.Fatalf("post-retarget version = %q, want 0.144.3", got.version)
	}
	firstGot := <-first
	assertResolvedExecutable(t, firstGot.entry.Path, stableExecutable, v2)
	if firstGot.version != "0.144.3" {
		t.Fatalf("pre-retarget caller final version = %q, want current 0.144.3", firstGot.version)
	}
}

func TestResolveAgentEntryForLaunchFailsWhenJunctionKeepsRetargeting(t *testing.T) {
	root := t.TempDir()
	releases := filepath.Join(root, "releases")
	installRelease := func(version string) string {
		bin := filepath.Join(releases, version, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		executable := filepath.Join(bin, "codex.exe")
		copyTestExecutable(t, executable)
		return executable
	}
	v1 := installRelease("0.144.1")
	v2 := installRelease("0.144.3")
	current := filepath.Join(root, "current")
	retarget := func(target string) {
		if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove current junction: %v", err)
		}
		createJunction(t, filepath.Dir(filepath.Dir(target)), current)
	}
	retarget(v1)
	visibleBin := filepath.Join(root, "visible", "bin")
	createJunction(t, filepath.Join(current, "bin"), visibleBin)
	stableExecutable := filepath.Join(visibleBin, "codex.exe")

	originalDetect := detectAgentVersion
	detectAgentVersion = func(_ context.Context, runtimeCmd agent.Command) (string, error) {
		path := runtimeCmd.Path
		if sameFile(path, v1) {
			retarget(v2)
			return "0.144.1", nil
		}
		if sameFile(path, v2) {
			retarget(v1)
			return "0.144.3", nil
		}
		return "", errors.New("unexpected version probe path")
	}
	t.Cleanup(func() { detectAgentVersion = originalDetect })

	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: stableExecutable, Command: stableExecutable}
	got, _, err := d.resolveAgentEntryForLaunch(context.Background(), "codex", entry)
	if err == nil {
		t.Fatalf("resolveAgentEntryForLaunch returned stale target %q while junction kept retargeting", got.Path)
	}
	if !strings.Contains(err.Error(), "changed repeatedly") {
		t.Fatalf("resolveAgentEntryForLaunch error = %v, want repeated-retarget failure", err)
	}
}

func TestResolveAgentEntryCanonicalizesRediscoveredJunction(t *testing.T) {
	originalDetect := detectAgentVersion
	detectAgentVersion = func(_ context.Context, runtimeCmd agent.Command) (string, error) {
		return "0.144.3", nil
	}
	t.Cleanup(func() { detectAgentVersion = originalDetect })

	targetExecutable, shimBin := stageJunctionExecutable(t)
	t.Setenv("PATH", shimBin)
	t.Setenv("PATHEXT", ".EXE")
	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: filepath.Join(t.TempDir(), "removed", "codex.exe"), Command: "codex"}

	got, version := d.resolveAgentEntry(context.Background(), "codex", entry)
	assertResolvedExecutable(t, got.Path, filepath.Join(shimBin, "codex.exe"), targetExecutable)
	if version != "0.144.3" {
		t.Fatalf("rediscovered version = %q, want 0.144.3", version)
	}
}

func TestResolveAgentEntryForLaunchRejectsRediscoveredJunctionWhenFinalPathResolutionFails(t *testing.T) {
	targetExecutable, shimBin := stageJunctionExecutable(t)
	stableExecutable := filepath.Join(shimBin, "codex.exe")
	t.Setenv("PATH", shimBin)
	t.Setenv("PATHEXT", ".EXE")

	originalDetect := detectAgentVersion
	detectCalled := false
	detectAgentVersion = func(context.Context, agent.Command) (string, error) {
		detectCalled = true
		return "0.144.3", nil
	}
	t.Cleanup(func() { detectAgentVersion = originalDetect })

	resolutionErr := errors.New("final path volume has no DOS name")
	originalLaunchResolver := executablePathForLaunch
	executablePathForLaunch = func(path string) (string, bool, error) {
		if strings.EqualFold(path, stableExecutable) {
			return "", true, resolutionErr
		}
		return originalLaunchResolver(path)
	}
	t.Cleanup(func() { executablePathForLaunch = originalLaunchResolver })

	var logs bytes.Buffer
	d := newSelfHealTestDaemon()
	d.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: filepath.Join(t.TempDir(), "removed", "codex.exe"), Command: "codex"}

	got, _, err := d.resolveAgentEntryForLaunch(context.Background(), "codex", entry)
	if err == nil {
		t.Fatalf("resolveAgentEntryForLaunch returned rediscovered unresolved path %q; want final-path failure", got.Path)
	}
	if !errors.Is(err, resolutionErr) {
		t.Fatalf("resolveAgentEntryForLaunch error = %v, want %v", err, resolutionErr)
	}
	if detectCalled {
		t.Fatal("detectAgentVersion was called for an unverified rediscovered launch target")
	}
	d.resolvedPathsMu.RLock()
	_, cached := d.resolvedPaths["codex"]
	d.resolvedPathsMu.RUnlock()
	if cached {
		t.Fatal("unverified rediscovered launch target was cached in resolvedPaths")
	}
	if strings.EqualFold(got.Path, stableExecutable) || sameFile(got.Path, targetExecutable) {
		t.Fatalf("resolveAgentEntryForLaunch returned a launchable target %q after resolution failure", got.Path)
	}

	logText := logs.String()
	if !strings.Contains(logText, "level=WARN") {
		t.Fatalf("rediscovery resolution failure log = %q, want WARN", logText)
	}
	if !strings.Contains(logText, stableExecutable) {
		t.Fatalf("rediscovery resolution failure log = %q, want path %q", logText, stableExecutable)
	}
	if !strings.Contains(logText, resolutionErr.Error()) {
		t.Fatalf("rediscovery resolution failure log = %q, want error %q", logText, resolutionErr)
	}
}

func TestCanonicalExecutablePathLaunchesBeyondMaxPath(t *testing.T) {
	deep := t.TempDir()
	for len(deep) < 280 {
		deep = filepath.Join(deep, "release-segment-0123456789")
	}
	deep = `\\?\` + deep
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("create long release path: %v", err)
	}

	target := filepath.Join(deep, "codex.exe")
	copyTestExecutable(t, target)
	shimBin := filepath.Join(t.TempDir(), "bin")
	createJunction(t, deep, shimBin)
	shimExecutable := filepath.Join(shimBin, "codex.exe")

	resolved := canonicalExecutablePath(shimExecutable)
	if !strings.HasPrefix(resolved, `\\?\`) {
		t.Fatalf("resolved long executable dropped extended-length prefix: %q", resolved)
	}
	assertLaunchesExecutable(t, resolved, target)
}

func TestResolveAgentExecutablePathKeepsConfiguredJunctionEntryPoint(t *testing.T) {
	_, shimBin := stageJunctionExecutable(t)
	configured := filepath.Join(shimBin, "codex.exe")

	got, err := resolveAgentExecutablePath(configured)
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	if !strings.EqualFold(got, configured) {
		t.Fatalf("resolved configured entry = %q, want stable junction entry %q", got, configured)
	}
}

func assertResolvedExecutable(t *testing.T, got, shim, target string) {
	t.Helper()
	if strings.EqualFold(got, shim) {
		t.Fatalf("resolved executable still points at junction path %q", got)
	}
	assertSameFile(t, got, target)
}

func assertSameFile(t *testing.T, got, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat resolved executable %q: %v", got, err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat expected executable %q: %v", want, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("resolved executable %q does not identify expected file %q", got, want)
	}
}

func sameFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func assertLaunchesExecutable(t *testing.T, executable, want string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "launched-from")
	cmd := exec.Command(executable, "-test.run=^TestLongPathExecutableHelper$")
	cmd.Env = append(os.Environ(),
		longPathExecutableHelperEnv+"=1",
		longPathExecutableMarkerEnv+"="+marker,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("launch executable %q: %v\n%s", executable, err, output)
	}
	launchedFrom, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read launched executable marker: %v", err)
	}
	assertSameFile(t, string(launchedFrom), want)
}

func copyTestExecutable(t *testing.T, target string) {
	t.Helper()
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
}

func stageJunctionExecutable(t *testing.T) (targetExecutable, shimBin string) {
	t.Helper()
	targetBin := filepath.Join(t.TempDir(), "release", "bin")
	if err := os.MkdirAll(targetBin, 0o755); err != nil {
		t.Fatal(err)
	}
	targetExecutable = filepath.Join(targetBin, "codex.exe")
	if err := os.WriteFile(targetExecutable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	shimBin = filepath.Join(t.TempDir(), "installer", "bin")
	createJunction(t, targetBin, shimBin)
	return targetExecutable, shimBin
}

// TestCanonicalExecutablePathLaunchesCmdShim covers the npm install shape from
// #6883: the discovered entry point is a `.cmd` shim (`%APPDATA%\npm\codex.cmd`),
// which Windows can only run by handing the path to the command interpreter.
// cmd.exe does not accept extended-length paths, so a launch target that keeps
// the `\\?\` prefix cannot be executed at all. Every other case in this file
// stages a real `.exe`, which hides that difference completely.
func TestCanonicalExecutablePathLaunchesCmdShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n> \"%~1\" echo launched\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolved := canonicalExecutablePath(shim)
	marker := filepath.Join(dir, "launched.txt")
	if output, err := exec.Command(resolved, marker).CombinedOutput(); err != nil {
		t.Fatalf("launch .cmd shim %q: %v\n%s", resolved, err, output)
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("resolved .cmd shim %q did not run: %v", resolved, err)
	}
}

// TestResolveAgentEntryForLaunchKeepsCmdShimLaunchable is the same property at
// the boundary that actually launches tasks. resolveAgentEntryForLaunch is what
// every built-in provider goes through on Windows, so a `.cmd` entry point has
// to survive it in runnable form.
func TestResolveAgentEntryForLaunchKeepsCmdShimLaunchable(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n> \"%~1\" echo launched\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveAgentExecutablePath(shim)
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	launchPath, handled, err := executablePathForLaunch(resolved)
	if !handled {
		t.Fatal("executablePathForLaunch reported unhandled on windows")
	}
	if err != nil {
		t.Fatalf("executablePathForLaunch: %v", err)
	}

	marker := filepath.Join(dir, "launched.txt")
	if output, err := exec.Command(launchPath, marker).CombinedOutput(); err != nil {
		t.Fatalf("launch resolved .cmd entry %q: %v\n%s", launchPath, err, output)
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("resolved .cmd entry %q did not run: %v", launchPath, err)
	}
}

// TestTrimExtendedLengthPrefix pins the rule: drop the prefix when the plain
// Win32 path can stand on its own, keep it when it is what makes the path
// nameable or when the remainder is not a drive-qualified path.
func TestTrimExtendedLengthPrefix(t *testing.T) {
	longTail := strings.Repeat(`segment\`, 40) + "codex.exe"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "short drive path drops the prefix",
			in:   `\\?\C:\Users\dev\AppData\Roaming\npm\codex.cmd`,
			want: `C:\Users\dev\AppData\Roaming\npm\codex.cmd`,
		},
		{
			name: "path past MAX_PATH keeps the prefix",
			in:   `\\?\C:\` + longTail,
			want: `\\?\C:\` + longTail,
		},
		{
			name: "unc path becomes a plain unc path",
			in:   `\\?\UNC\server\share\bin\codex.cmd`,
			want: `\\server\share\bin\codex.cmd`,
		},
		{
			// 100 CJK characters: 300 UTF-8 bytes but only 100 UTF-16 code
			// units, so the whole path is ~123 units — comfortably inside
			// MAX_PATH. Measuring bytes would call this too long and keep a
			// prefix that breaks .cmd launches for anyone whose user directory
			// is not ASCII.
			name: "non-ascii path is measured in utf-16 code units",
			in:   `\\?\C:\Users\` + strings.Repeat("目录", 50) + `\npm\codex.cmd`,
			want: `C:\Users\` + strings.Repeat("目录", 50) + `\npm\codex.cmd`,
		},
		{
			name: "volume guid keeps the prefix",
			in:   `\\?\Volume{b75e2c83-0000-0000-0000-602f00000000}\bin\codex.exe`,
			want: `\\?\Volume{b75e2c83-0000-0000-0000-602f00000000}\bin\codex.exe`,
		},
		{
			name: "plain path is untouched",
			in:   `C:\bin\codex.exe`,
			want: `C:\bin\codex.exe`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimExtendedLengthPrefix(tc.in); got != tc.want {
				t.Fatalf("trimExtendedLengthPrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
