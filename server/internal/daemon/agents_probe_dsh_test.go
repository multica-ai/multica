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
