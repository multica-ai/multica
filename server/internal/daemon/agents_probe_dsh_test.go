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
			if got := probeDshMulticaProfile(path, nil); got != tc.want {
				t.Fatalf("probeDshMulticaProfile() = %v, want %v", got, tc.want)
			}
		})
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
	if err := os.WriteFile(filepath.Join(trustedBin, "node"), []byte("#!/bin/sh\nprintf '%s\\n' '{\"v\":1,\"type\":\"probe\",\"runtime\":\"dsh\",\"protocol_version\":1}'\n"), 0o755); err != nil {
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
