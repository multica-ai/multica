package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
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

func TestProbeAgentCLIsLinuxZcodeDesktop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix desktop entry point")
	}
	for _, source := range []string{"PATH", "login shell", "override", "default install"} {
		t.Run(source, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "ZCode install", "zcode")
			script := filepath.Join(filepath.Dir(bin), "resources", "glm", "zcode.cjs")
			if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(script, []byte("// bundled CLI\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
[ "$ELECTRON_RUN_AS_NODE" = 1 ] && [ -f "$1" ] && [ "$2" = --version ] || exit 42
printf '0.17.1\n'
`), 0o755); err != nil {
				t.Fatal(err)
			}
			origPaths := zcodeDesktopAppBundlePaths
			t.Cleanup(func() { zcodeDesktopAppBundlePaths = origPaths })
			zcodeDesktopAppBundlePaths = func() []string {
				if source == "default install" {
					return []string{bin}
				}
				return nil
			}
			origResolve := resolveAgentsViaLoginShell
			t.Cleanup(func() { resolveAgentsViaLoginShell = origResolve })
			resolveAgentsViaLoginShell = func([]string) map[string]string { return nil }
			resetShellResolveCacheForTest(t)
			t.Setenv("PATH", root)
			t.Setenv("MULTICA_ZCODE_PATH", "")
			t.Setenv("MULTICA_ZCODE_MODEL", "zai/glm-5.3")
			switch source {
			case "PATH":
				link := filepath.Join(root, "zcode")
				if err := os.Symlink(bin, link); err != nil {
					t.Fatal(err)
				}
			case "login shell":
				resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{"zcode": bin} }
			case "override":
				t.Setenv("MULTICA_ZCODE_PATH", bin)
			}
			entry, ok := probeAgentCLIs()["zcode"]
			if !ok || entry.Path != bin || entry.Model != "zai/glm-5.3" {
				t.Fatalf("discovered ZCode = %+v, present = %v", entry, ok)
			}
			version, err := agent.DetectVersion(context.Background(), agent.NewCommand(entry.Path, nil).ForVersionProbe("zcode"))
			if err != nil || version != "0.17.1" {
				t.Fatalf("version = %q, err = %v", version, err)
			}
		})
	}
}
