package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// TestDaemonAlive locks in the liveness predicate the lifecycle commands rely
// on: both a ready ("running") and a still-booting ("starting") daemon count as
// alive, so `daemon start` won't double-spawn over a starting daemon and
// `restart`/`stop` will act on one; only "stopped"/unknown is "no daemon".
func TestDaemonAlive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status any
		want   bool
	}{
		{"running", true},
		{"starting", true},
		{"stopped", false},
		{"", false},
		{nil, false},
		{"bogus", false},
	}
	for _, c := range cases {
		if got := daemonAlive(map[string]any{"status": c.status}); got != c.want {
			t.Errorf("daemonAlive(status=%v) = %v, want %v", c.status, got, c.want)
		}
	}
	// A response with no status key at all (e.g. malformed) is not alive.
	if daemonAlive(map[string]any{}) {
		t.Errorf("daemonAlive(no status) = true, want false")
	}
}

func TestPrintDaemonStatusIncludesCLIVersion(t *testing.T) {
	t.Parallel()

	health := map[string]any{
		"status":      "running",
		"pid":         float64(1234),
		"uptime":      "1h2m3s",
		"cli_version": "v9.9.9",
		"agents":      []any{"codex"},
		"workspaces":  []any{map[string]any{"id": "ws-1"}},
	}

	var out bytes.Buffer
	printDaemonStatusReport(&out, "Daemon", health)

	got := out.String()
	if !strings.Contains(got, "Version:     v9.9.9\n") {
		t.Fatalf("daemon status output = %q, want CLI version line", got)
	}
}

// TestPrintDaemonStatusOmitsVersionWhenMissing pins the back-compat contract:
// when the daemon doesn't report cli_version (older daemon paired with a newer
// CLI) or reports an empty string, the CLI must skip the line entirely instead
// of printing "Version: ".
func TestPrintDaemonStatusOmitsVersionWhenMissing(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"key missing": {
			"status":     "running",
			"pid":        float64(1234),
			"uptime":     "1h2m3s",
			"workspaces": []any{},
		},
		"empty string": {
			"status":      "running",
			"pid":         float64(1234),
			"uptime":      "1h2m3s",
			"cli_version": "",
			"workspaces":  []any{},
		},
	}

	for name, health := range cases {
		health := health
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			printDaemonStatusReport(&out, "Daemon", health)
			if strings.Contains(out.String(), "Version:") {
				t.Fatalf("daemon status output = %q, want no Version line", out.String())
			}
		})
	}
}

// TestPrintDaemonStatusAlignsValuesWithProfileLabel guards the alignment fix:
// before, a "Daemon [profile]" label was wider than the other keys, so the
// Daemon row's value started further right than every subsequent row. The
// report now pads every key to the widest one, so the value column lines up.
func TestPrintDaemonStatusAlignsValuesWithProfileLabel(t *testing.T) {
	t.Parallel()

	health := map[string]any{
		"status":      "running",
		"pid":         float64(1234),
		"uptime":      "1h2m3s",
		"cli_version": "v9.9.9",
		"agents":      []any{"codex"},
		"workspaces":  []any{map[string]any{"id": "ws-1"}},
	}

	var out bytes.Buffer
	printDaemonStatusReport(&out, "Daemon [staging]", health)

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %q", out.String())
	}

	// Find the column where each row's value starts (first non-space after
	// the colon). Every row must share the same column.
	want := valueColumn(t, lines[0])
	for _, line := range lines[1:] {
		if got := valueColumn(t, line); got != want {
			t.Fatalf("value column drift: line %q starts at col %d, want %d (first line: %q)",
				line, got, want, lines[0])
		}
	}
}

func TestPrintDiskUsageEmptyHintSuggestsProfilesWithTasks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	mkdirProfile(t, home, "empty")
	mkdirProfile(t, home, "one-task")
	mkdirProfile(t, home, "space profile")
	mkdirProfile(t, home, "two-tasks")

	writeDiskUsageTaskFile(t, home, "one-task", "ws1", "task1", "workdir/main.go")
	writeDiskUsageTaskFile(t, home, "space profile", "ws3", "task1", "workdir/main.go")
	writeDiskUsageTaskFile(t, home, "two-tasks", "ws2", "task1", "workdir/main.go")
	writeDiskUsageTaskFile(t, home, "two-tasks", "ws2", "task2", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageEmptyHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "multica_workspaces"),
	}, "", "")

	got := out.String()
	if !strings.Contains(got, "Other workspace roots contain task directories:") {
		t.Fatalf("hint output = %q, want profile suggestion header", got)
	}
	if !strings.Contains(got, "multica --profile two-tasks daemon disk-usage") {
		t.Fatalf("hint output = %q, want two-tasks profile command", got)
	}
	if !strings.Contains(got, "multica --profile one-task daemon disk-usage") {
		t.Fatalf("hint output = %q, want one-task profile command", got)
	}
	if !strings.Contains(got, "multica --profile 'space profile' daemon disk-usage") {
		t.Fatalf("hint output = %q, want shell-quoted profile command", got)
	}
	if strings.Contains(got, "empty") {
		t.Fatalf("hint output = %q, want empty profile omitted", got)
	}
	if strings.Index(got, "two-tasks") > strings.Index(got, "one-task") {
		t.Fatalf("hint output = %q, want larger profile first", got)
	}
}

func TestPrintDiskUsageEmptyHintSuggestsDefaultFromNamedProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	writeDefaultDiskUsageTaskFile(t, home, "ws0", "task0", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageEmptyHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "multica_workspaces_named"),
	}, "named", "")

	got := out.String()
	if !strings.Contains(got, "multica daemon disk-usage") {
		t.Fatalf("hint output = %q, want default profile command", got)
	}
	if strings.Contains(got, "--profile") {
		t.Fatalf("hint output = %q, want default profile command without --profile", got)
	}
}

func TestPrintDiskUsageEmptyHintSkipsExplicitRootOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	mkdirProfile(t, home, "has-task")
	writeDiskUsageTaskFile(t, home, "has-task", "ws1", "task1", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageEmptyHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "custom-root"),
	}, "", filepath.Join(home, "custom-root"))

	if got := out.String(); got != "" {
		t.Fatalf("hint output = %q, want no hint for explicit root override", got)
	}
}

func valueColumn(t *testing.T, line string) int {
	t.Helper()
	colon := strings.Index(line, ":")
	if colon < 0 {
		t.Fatalf("line missing colon: %q", line)
	}
	for i := colon + 1; i < len(line); i++ {
		if line[i] != ' ' {
			return i
		}
	}
	t.Fatalf("line missing value: %q", line)
	return 0
}

func mkdirProfile(t *testing.T, home, profile string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".multica", "profiles", profile), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeDiskUsageTaskFile(t *testing.T, home, profile, workspaceID, taskID, rel string) {
	t.Helper()
	path := filepath.Join(home, "multica_workspaces_"+profile, workspaceID, taskID, rel)
	writeDiskUsageFile(t, path)
}

func writeDefaultDiskUsageTaskFile(t *testing.T, home, workspaceID, taskID, rel string) {
	t.Helper()
	path := filepath.Join(home, "multica_workspaces", workspaceID, taskID, rel)
	writeDiskUsageFile(t, path)
}

func writeDiskUsageFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
