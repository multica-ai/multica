package daemon

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestProbeAgentCLIsRequiresDshMulticaProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	originalResolver := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = originalResolver })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "profile installed", body: `printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","protocol_version":1}'`, want: true},
		{name: "profile missing", body: `printf '%s\n' 'missing multica profile'; exit 1`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeDir := t.TempDir()
			path := filepath.Join(fakeDir, "dsh")
			if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+tc.body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeDir)
			t.Setenv("MULTICA_DSH_PATH", "")
			_, found := probeAgentCLIs()["dsh"]
			if found != tc.want {
				t.Fatalf("dsh discovered = %v, want %v", found, tc.want)
			}
		})
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
