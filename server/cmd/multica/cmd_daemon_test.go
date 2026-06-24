package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestDaemonInstancePaths_UseExplicitConfigDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "envs", "local", "config.json")

	dir := daemonDirForInstance("", configPath)
	if dir != filepath.Dir(configPath) {
		t.Fatalf("daemonDirForInstance() = %q, want %q", dir, filepath.Dir(configPath))
	}
	if got := daemonPIDPathForInstance("", configPath); got != filepath.Join(filepath.Dir(configPath), "daemon.pid") {
		t.Fatalf("daemonPIDPathForInstance() = %q", got)
	}
	if got := daemonLogPathForInstance("", configPath); got != filepath.Join(filepath.Dir(configPath), "daemon.log") {
		t.Fatalf("daemonLogPathForInstance() = %q", got)
	}
}

func TestHealthPortForInstance_IsolatedByConfigPath(t *testing.T) {
	configA := filepath.Join(t.TempDir(), "envs", "a", "config.json")
	configB := filepath.Join(t.TempDir(), "envs", "b", "config.json")

	portA := healthPortForInstance("", configA)
	portB := healthPortForInstance("", configB)
	if portA == portB {
		t.Fatalf("expected distinct ports for distinct config paths, got %d", portA)
	}
}

func TestBuildDaemonStartArgs_ForwardsConfigAndProfile(t *testing.T) {
	cmd := testCmd()
	configPath := filepath.Join(t.TempDir(), "envs", "local", "config.json")
	if err := cmd.Flags().Set("config", configPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if err := cmd.Flags().Set("profile", "dev"); err != nil {
		t.Fatalf("set profile: %v", err)
	}

	got := buildDaemonStartArgs(cmd)
	want := []string{"daemon", "start", "--foreground", "--config", configPath, "--profile", "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDaemonStartArgs() = %v, want %v", got, want)
	}
}

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
