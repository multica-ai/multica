package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestChooseCopilotInvocation_PassthroughForNonLauncher verifies that when
// the resolved executable is not a Windows .cmd/.bat launcher, both argv[0]
// and the argv list are returned unchanged on every platform. This guards
// against accidental rewriting on macOS/Linux and for direct binary launches
// on Windows.
func TestChooseCopilotInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "copilot"
	lookedUp := filepath.Join(t.TempDir(), "copilot") // no .cmd / .bat
	args := []string{
		"-p", "You are running as a local coding agent.\n\nDo something.",
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}

	gotExec, gotArgs := chooseCopilotInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}

// ── Windows native-binary resolution tests ──
//
// These run on every platform: resolveCopilotNativeFromShim takes statFn as a
// parameter precisely so the Windows layout can be asserted from macOS/Linux CI.

// TestResolveCopilotNativeFromShim_NestedNpmLayout covers the layout npm
// actually produces today (verified against @github/copilot 1.0.77): the
// platform package stays nested under the parent package rather than being
// hoisted.
func TestResolveCopilotNativeFromShim_NestedNpmLayout(t *testing.T) {
	t.Parallel()

	prefix := filepath.Join("C:\\Users", "dev", "AppData", "Roaming", "npm")
	shim := filepath.Join(prefix, "copilot.cmd")
	native := filepath.Join(prefix, "node_modules", "@github", "copilot", "node_modules", "@github", "copilot-win32-x64", "copilot.exe")

	if got := resolveCopilotNativeFromShim(shim, fakeStat(native)); got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

// TestResolveCopilotNativeFromShim_HoistedNpmLayout covers older npm versions
// and other installers that hoist the optional platform dep to the top-level
// node_modules.
func TestResolveCopilotNativeFromShim_HoistedNpmLayout(t *testing.T) {
	t.Parallel()

	prefix := filepath.Join("C:\\Users", "dev", "AppData", "Roaming", "npm")
	shim := filepath.Join(prefix, "copilot.cmd")
	native := filepath.Join(prefix, "node_modules", "@github", "copilot-win32-x64", "copilot.exe")

	if got := resolveCopilotNativeFromShim(shim, fakeStat(native)); got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

// TestResolveCopilotNativeFromShim_FindsArm64Package covers Windows-on-ARM
// hosts, where npm installs @github/copilot-win32-arm64 instead. The resolver
// must find it regardless of which arch this test binary was built for, since
// the arch list only controls probe order, not membership.
func TestResolveCopilotNativeFromShim_FindsArm64Package(t *testing.T) {
	t.Parallel()

	prefix := filepath.Join("C:\\Users", "dev", "AppData", "Roaming", "npm")
	shim := filepath.Join(prefix, "copilot.cmd")
	native := filepath.Join(prefix, "node_modules", "@github", "copilot", "node_modules", "@github", "copilot-win32-arm64", "copilot.exe")

	if got := resolveCopilotNativeFromShim(shim, fakeStat(native)); got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

// TestResolveCopilotNativeFromShim_ReturnsEmptyWhenNativeMissing covers a
// partial install or a Copilot build predating the platform packages. The
// caller must fall back to the PowerShell launcher rather than spawn a path
// that doesn't exist.
func TestResolveCopilotNativeFromShim_ReturnsEmptyWhenNativeMissing(t *testing.T) {
	t.Parallel()

	shim := filepath.Join("C:\\Users", "dev", "AppData", "Roaming", "npm", "copilot.cmd")

	if got := resolveCopilotNativeFromShim(shim, fakeStat()); got != "" {
		t.Errorf("got %q, want empty (missing native binary)", got)
	}
}

// TestResolveCopilotNativeFromShim_SkipsNonCmdPath keeps macOS/Linux and
// direct-binary Windows launches on the untouched passthrough path.
func TestResolveCopilotNativeFromShim_SkipsNonCmdPath(t *testing.T) {
	t.Parallel()

	for _, p := range []string{
		"/usr/local/bin/copilot",
		"C:\\Users\\dev\\AppData\\Roaming\\npm\\copilot.exe",
		"C:\\Users\\dev\\AppData\\Roaming\\npm\\copilot.ps1",
		"",
	} {
		if got := resolveCopilotNativeFromShim(p, fakeStat("anything")); got != "" {
			t.Errorf("path %q: got %q, want empty", p, got)
		}
	}
}

// TestResolveCopilotNativeFromShim_AcceptsUppercaseExtension guards the
// PATHEXT case: Windows filesystems are case-insensitive and exec.LookPath
// can return either case.
func TestResolveCopilotNativeFromShim_AcceptsUppercaseExtension(t *testing.T) {
	t.Parallel()

	prefix := filepath.Join("C:\\Users", "dev", "AppData", "Roaming", "npm")
	shim := filepath.Join(prefix, "copilot.CMD")
	native := filepath.Join(prefix, "node_modules", "@github", "copilot", "node_modules", "@github", "copilot-win32-x64", "copilot.exe")

	if got := resolveCopilotNativeFromShim(shim, fakeStat(native)); got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

// TestCopilotWindowsPackageCandidates_ArchOrdering pins the probe order: the
// host's own arch first, but never at the cost of dropping the other one — an
// x64 Node running under emulation on an ARM64 host installs the x64 package.
func TestCopilotWindowsPackageCandidates_ArchOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		goarch string
		want   []string
	}{
		{"arm64", []string{"copilot-win32-arm64", "copilot-win32-x64"}},
		{"amd64", []string{"copilot-win32-x64", "copilot-win32-arm64"}},
	}
	for _, tc := range cases {
		if got := copilotWindowsPackageCandidates(tc.goarch); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("goarch %q: got %#v, want %#v", tc.goarch, got, tc.want)
		}
	}
}
