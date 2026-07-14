package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/spf13/cobra"
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

func TestBuildDaemonStartArgsForwardsCodexHandshakeTimeout(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Duration("codex-handshake-timeout", 0, "")
	if err := cmd.Flags().Set("codex-handshake-timeout", "42s"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	args := buildDaemonStartArgs(cmd)
	want := []string{"daemon", "start", "--foreground", "--codex-handshake-timeout", (42 * time.Second).String()}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("buildDaemonStartArgs() = %q, want %q", args, want)
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

func TestPrintDiskUsageOtherRootsHintSuggestsProfilesWithTasks(t *testing.T) {
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
	printDiskUsageOtherRootsHint(&out, daemon.DiskUsageReport{
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
	if !strings.Contains(got, "multica daemon disk-usage --all-profiles") {
		t.Fatalf("hint output = %q, want --all-profiles tip", got)
	}
	if strings.Contains(got, "(0 task") {
		t.Fatalf("hint output = %q, want empty profile omitted", got)
	}
	if strings.Index(got, "two-tasks") > strings.Index(got, "one-task") {
		t.Fatalf("hint output = %q, want larger profile first", got)
	}
}

// TestPrintDiskUsageOtherRootsHintFiresWhenCurrentRootNonEmpty is the core
// MUL-3404 behavior: the hint must surface other roots even when the scanned
// root already has tasks, otherwise the Desktop app's root stays hidden behind
// a non-empty default root.
func TestPrintDiskUsageOtherRootsHintFiresWhenCurrentRootNonEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	mkdirProfile(t, home, "desktop-host")
	writeDiskUsageTaskFile(t, home, "desktop-host", "ws1", "task1", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageOtherRootsHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "multica_workspaces"),
		TotalTaskCount: 7, // current root is NOT empty
	}, "", "")

	got := out.String()
	if !strings.Contains(got, "multica --profile desktop-host daemon disk-usage") {
		t.Fatalf("hint output = %q, want desktop-host suggestion even with a non-empty current root", got)
	}
}

func TestPrintDiskUsageOtherRootsHintSuggestsDefaultFromNamedProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	writeDefaultDiskUsageTaskFile(t, home, "ws0", "task0", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageOtherRootsHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "multica_workspaces_named"),
	}, "named", "")

	got := out.String()
	if !strings.Contains(got, "multica daemon disk-usage  #") {
		t.Fatalf("hint output = %q, want default profile command", got)
	}
}

func TestPrintDiskUsageOtherRootsHintSkipsExplicitRootOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	mkdirProfile(t, home, "has-task")
	writeDiskUsageTaskFile(t, home, "has-task", "ws1", "task1", "workdir/main.go")

	var out bytes.Buffer
	printDiskUsageOtherRootsHint(&out, daemon.DiskUsageReport{
		WorkspacesRoot: filepath.Join(home, "custom-root"),
	}, "", filepath.Join(home, "custom-root"))

	if got := out.String(); got != "" {
		t.Fatalf("hint output = %q, want no hint for explicit root override", got)
	}
}

func TestEnumerateDiskUsageRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	// Two profiles configured under ~/.multica/profiles, but only one has its
	// workspaces root created on disk; the other (never-run) profile is skipped.
	mkdirProfile(t, home, "desktop-host")
	mkdirProfile(t, home, "never-ran")
	writeDiskUsageTaskFile(t, home, "desktop-host", "ws1", "task1", "workdir/main.go")
	writeDefaultDiskUsageTaskFile(t, home, "ws0", "task0", "workdir/main.go")

	roots, err := enumerateDiskUsageRoots()
	if err != nil {
		t.Fatalf("enumerateDiskUsageRoots: %v", err)
	}

	if len(roots) != 2 {
		t.Fatalf("roots = %+v, want default + desktop-host only", roots)
	}
	if roots[0].Profile != "" || roots[0].Root != filepath.Join(home, "multica_workspaces") {
		t.Fatalf("roots[0] = %+v, want default root first", roots[0])
	}
	if roots[1].Profile != "desktop-host" || roots[1].Root != filepath.Join(home, "multica_workspaces_desktop-host") {
		t.Fatalf("roots[1] = %+v, want desktop-host root", roots[1])
	}
}

func TestPrintAggregateDiskUsageShowsRootsAndGrandTotal(t *testing.T) {
	agg := daemon.AggregateDiskUsageReport{
		Roots: []daemon.RootDiskUsage{
			{Profile: "", Report: daemon.DiskUsageReport{
				WorkspacesRoot: "/home/u/multica_workspaces",
				Tasks:          []daemon.TaskDiskUsage{{WorkspaceShort: "ws0", TaskShort: "t0", SizeBytes: 100}},
				TotalTaskCount: 1,
				TotalSizeBytes: 100,
			}},
			{Profile: "desktop-host", Report: daemon.DiskUsageReport{
				WorkspacesRoot: "/home/u/multica_workspaces_desktop-host",
				Tasks:          []daemon.TaskDiskUsage{{WorkspaceShort: "ws1", TaskShort: "t1", SizeBytes: 900}},
				TotalTaskCount: 1,
				TotalSizeBytes: 900,
			}},
		},
		TotalTaskCount: 2,
		TotalSizeBytes: 1000,
	}

	var out bytes.Buffer
	printAggregateDiskUsage(&out, agg, false)
	got := out.String()

	if !strings.Contains(got, "Scanned 2 workspace root(s).") {
		t.Fatalf("output = %q, want scanned-roots header", got)
	}
	if !strings.Contains(got, "[default]") || !strings.Contains(got, "[desktop-host]") {
		t.Fatalf("output = %q, want per-root section labels", got)
	}
	if !strings.Contains(got, "/home/u/multica_workspaces_desktop-host") {
		t.Fatalf("output = %q, want desktop root path", got)
	}
	if !strings.Contains(got, "Grand total:") || !strings.Contains(got, "across 2 task(s) in 2 root(s)") {
		t.Fatalf("output = %q, want grand total line", got)
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
