package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// writeDshProbeFixture writes a fake `dsh` launcher that prints body to stdout
// and exits with wantExit. On Windows the launcher is a .cmd file — the same
// shape npm-installed dsh resolves to — so the probe path
// (exec.CommandContext over a .cmd shim) is exercised on Windows too, not just
// POSIX shells.
func writeDshProbeFixture(t *testing.T, body string, wantExit int) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "dsh.cmd")
		content := "@echo off\r\n" + body + "\r\nexit /b " + strconv.Itoa(wantExit) + "\r\n"
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "dsh")
	script := "#!/bin/sh\nset -eu\n" + body + "\nexit " + strconv.Itoa(wantExit) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// dshProbeBody wraps a probe-response line in the platform's echo primitive.
func dshProbeBody(line string) string {
	if runtime.GOOS == "windows" {
		return "echo " + line
	}
	return "printf '%s\n' '" + line + "'"
}

func TestProbeDshMulticaProfile(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "compatible", body: `{"v":1,"type":"probe","runtime":"dsh","plugin_version":"test","protocol_version":1}`, want: true},
		{name: "missing plugin", body: `profile not installed`, want: false},
		{name: "future protocol", body: `{"v":2,"type":"probe","runtime":"dsh","protocol_version":2}`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeDshProbeFixture(t, dshProbeBody(tc.body), 0)
			if got := probeDshMulticaProfile(path); got != tc.want {
				t.Fatalf("probeDshMulticaProfile() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestProbeAgentCLIsRequiresDshMulticaProfile locks in the discovery gate: a
// bare dsh binary is not a Multica runtime — without the profile the bundle
// has no --stdio protocol and every task would fail after being advertised as
// healthy. The rejection must be reported through the discovery skip set so a
// host with dsh but no profile is distinguishable from one with no dsh at all.
func TestProbeAgentCLIsRequiresDshMulticaProfile(t *testing.T) {
	originalResolver := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = originalResolver })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "profile installed", body: `{"v":1,"type":"probe","runtime":"dsh","protocol_version":1}`, want: true},
		{name: "profile missing", body: `missing multica profile`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeDir := filepath.Dir(writeDshProbeFixture(t, dshProbeBody(tc.body), 0))
			t.Setenv("PATH", fakeDir)
			t.Setenv("MULTICA_DSH_PATH", "")
			agents, skipped := probeAgentCLIs()
			_, found := agents["dsh"]
			if found != tc.want {
				t.Fatalf("dsh discovered = %v, want %v", found, tc.want)
			}
			reason, reported := skipped["dsh"]
			if tc.want {
				if reported {
					t.Errorf("dsh with a working profile should not be skipped, got reason %q", reason)
				}
				return
			}
			if !reported {
				t.Fatal("dsh without its Multica profile should be reported in the discovery skip set, got none")
			}
			if !strings.Contains(reason, "dsh plugin --profile multica add") {
				t.Fatalf("dsh skip reason = %q, want it to name the repair command", reason)
			}
		})
	}
}
