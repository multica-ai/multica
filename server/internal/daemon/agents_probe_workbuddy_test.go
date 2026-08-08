//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func stageWorkBuddyCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "WorkBuddy.app", "Contents", "Resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fake WorkBuddy bundle: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake WorkBuddy CLI: %v", err)
	}
	return path
}

func stubWorkBuddyDiscovery(t *testing.T, paths []string) {
	t.Helper()
	home := t.TempDir()
	nodePath := filepath.Join(home, ".workbuddy", "binaries", "node", "versions", "22.22.2", "bin", "node")
	if err := os.MkdirAll(filepath.Dir(nodePath), 0o755); err != nil {
		t.Fatalf("create fake WorkBuddy Node runtime: %v", err)
	}
	if err := os.WriteFile(nodePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake WorkBuddy Node runtime: %v", err)
	}
	originalBundlePaths := workbuddyDesktopAppBundlePaths
	originalShellResolver := resolveAgentsViaLoginShell
	workbuddyDesktopAppBundlePaths = func() []string { return paths }
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	t.Cleanup(func() {
		workbuddyDesktopAppBundlePaths = originalBundlePaths
		resolveAgentsViaLoginShell = originalShellResolver
	})
	resetShellResolveCacheForTest(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_CODEBUDDY_PATH", "")
}

func TestProbeAgentCLIs_UsesWorkBuddyAppBundleFallback(t *testing.T) {
	fakeCodeBuddy := stageWorkBuddyCLI(t)
	stubWorkBuddyDiscovery(t, []string{fakeCodeBuddy})
	t.Setenv("MULTICA_CODEBUDDY_MODEL", "deepseek-v3-2-volc")

	entry, ok := probeAgentCLIs()["codebuddy"]
	if !ok {
		t.Fatal("codebuddy was not discovered from the WorkBuddy app bundle")
	}
	if entry.Path != fakeCodeBuddy {
		t.Fatalf("codebuddy path = %q, want WorkBuddy CLI %q", entry.Path, fakeCodeBuddy)
	}
	if entry.Command != "codebuddy" {
		t.Fatalf("codebuddy command = %q, want codebuddy", entry.Command)
	}
	if entry.Model != "deepseek-v3-2-volc" {
		t.Fatalf("codebuddy model = %q, want deepseek-v3-2-volc", entry.Model)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".workbuddy", "binaries", "node", "versions", "22.22.2", "bin", "node")); err != nil {
		t.Fatalf("staged WorkBuddy Node runtime is missing: %v", err)
	}
	if got := filepath.SplitList(os.Getenv("PATH"))[0]; got != filepath.Join(os.Getenv("HOME"), ".workbuddy", "binaries", "node", "versions", "22.22.2", "bin") {
		t.Fatalf("PATH = %q, want WorkBuddy Node directory first", os.Getenv("PATH"))
	}
}

func TestProbeAgentCLIs_CodeBuddyPathPrecedesWorkBuddyBundle(t *testing.T) {
	fakeBundleCLI := stageWorkBuddyCLI(t)
	stubWorkBuddyDiscovery(t, []string{fakeBundleCLI})

	pathCLI := filepath.Join(t.TempDir(), "codebuddy")
	if err := os.WriteFile(pathCLI, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write PATH CodeBuddy CLI: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(pathCLI))

	entry, ok := probeAgentCLIs()["codebuddy"]
	if !ok {
		t.Fatal("codebuddy was not discovered from PATH")
	}
	if entry.Path != canonicalExecutablePath(pathCLI) {
		t.Fatalf("codebuddy path = %q, want PATH CLI %q", entry.Path, canonicalExecutablePath(pathCLI))
	}
}

func TestProbeAgentCLIs_MissingExplicitCodeBuddyPathDoesNotUseWorkBuddyBundle(t *testing.T) {
	fakeCodeBuddy := stageWorkBuddyCLI(t)
	stubWorkBuddyDiscovery(t, []string{fakeCodeBuddy})
	t.Setenv("MULTICA_CODEBUDDY_PATH", filepath.Join(t.TempDir(), "missing-codebuddy"))

	if entry, ok := probeAgentCLIs()["codebuddy"]; ok {
		t.Fatalf("missing explicit MULTICA_CODEBUDDY_PATH fell back to %q", entry.Path)
	}
}

func TestProbeAgentCLIs_WorkBuddyBundleRequiresNodeRuntime(t *testing.T) {
	fakeCodeBuddy := stageWorkBuddyCLI(t)
	stubWorkBuddyDiscovery(t, []string{fakeCodeBuddy})
	originalNodePaths := workbuddyNodeBinaryPaths
	workbuddyNodeBinaryPaths = func() []string { return nil }
	t.Cleanup(func() { workbuddyNodeBinaryPaths = originalNodePaths })

	if entry, ok := probeAgentCLIs()["codebuddy"]; ok {
		t.Fatalf("WorkBuddy bundle without a Node runtime was registered at %q", entry.Path)
	}
}

func TestWorkBuddyDesktopAppBundlePaths_PrefersSystemInstallation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths := workbuddyDesktopAppBundlePaths()
	wantSystem := "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy"
	wantUser := filepath.Join(home, "Applications", "WorkBuddy.app", "Contents", "Resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if len(paths) != 2 {
		t.Fatalf("WorkBuddy bundle paths = %#v, want system and user candidates", paths)
	}
	if paths[0] != wantSystem || paths[1] != wantUser {
		t.Fatalf("WorkBuddy bundle paths = %#v, want [%q %q]", paths, wantSystem, wantUser)
	}
}
