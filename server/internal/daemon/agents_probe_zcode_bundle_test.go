package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeAgentCLIsDiscoversZCodeAppBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the ZCode app-bundle fallback is a macOS layout")
	}
	origBundlePaths := zcodeDesktopAppBundlePaths
	t.Cleanup(func() { zcodeDesktopAppBundlePaths = origBundlePaths })
	bundle := filepath.Join(t.TempDir(), "ZCode.app", "Contents", "Resources", "glm", "zcode.cjs")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
		t.Fatal(err)
	}
	// A Node script with the executable bit — exactly what the app bundle
	// ships, and what executableCandidate requires on Unix.
	if err := os.WriteFile(bundle, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	zcodeDesktopAppBundlePaths = func() []string { return []string{bundle} }

	origResolve := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = origResolve })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_ZCODE_PATH", "")

	entry, ok := probeAgentCLIs()["zcode"]
	if !ok {
		t.Fatal("ZCode was not discovered from its app-bundle fallback path")
	}
	if filepath.Clean(entry.Path) != filepath.Clean(bundle) {
		t.Fatalf("ZCode path = %q, want %q", entry.Path, bundle)
	}
	if entry.Command != "zcode" {
		t.Fatalf("unexpected ZCode entry: %+v", entry)
	}
	if got := providerDisplayName("zcode"); got != "ZCode" {
		t.Fatalf("ZCode display name = %q, want ZCode", got)
	}
}
