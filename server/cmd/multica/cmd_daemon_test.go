package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
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

// TestRequireDaemonAuth pins the fail-fast contract for `daemon start`: a
// user who never ran `multica login` must get an immediate, actionable error
// from the parent process instead of a 45s health poll against a child that
// already died with "not authenticated".
func TestRequireDaemonAuth(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		err := requireDaemonAuth("")
		if err == nil || !strings.Contains(err.Error(), "multica login") {
			t.Fatalf("requireDaemonAuth() = %v, want error mentioning 'multica login'", err)
		}
	})

	t.Run("not logged in with profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		err := requireDaemonAuth("staging")
		if err == nil || !strings.Contains(err.Error(), "multica login --profile staging") {
			t.Fatalf("requireDaemonAuth(staging) = %v, want error mentioning profile login hint", err)
		}
	})

	t.Run("authenticated", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := cli.SaveCLIConfig(cli.CLIConfig{Token: "mul_test_token"}); err != nil {
			t.Fatalf("SaveCLIConfig: %v", err)
		}
		if err := requireDaemonAuth(""); err != nil {
			t.Fatalf("requireDaemonAuth() = %v, want nil", err)
		}
	})
}

// TestDaemonStartBackgroundUnauthenticatedFailsFast exercises the real
// `daemon start` background path: without a stored token it must error out
// before spawning the child (and long before the 45s readiness wait).
func TestDaemonStartBackgroundUnauthenticatedFailsFast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().Bool("foreground", false, "")
	cmd.Flags().String("profile", "", "")
	// A named profile keeps the pre-spawn health probe off the default port,
	// so a daemon running on the developer machine can't turn this into an
	// "already running" error.
	if err := cmd.Flags().Set("profile", "authtest-fail-fast"); err != nil {
		t.Fatalf("set profile flag: %v", err)
	}

	start := time.Now()
	err := runDaemonStart(cmd, nil)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "multica login --profile authtest-fail-fast") {
		t.Fatalf("runDaemonStart() = %v, want not-logged-in error with login hint", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("runDaemonStart took %s, want fail-fast before the readiness wait", elapsed)
	}
}

// TestReadLogTailSince pins the log-excerpt helper used when the daemon child
// dies during startup: only content appended after the recorded offset is
// shown, capped to the last maxLines lines.
func TestReadLogTailSince(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(logPath, []byte("old line 1\nold line 2\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	offset := info.Size()

	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(f, "new line %d\n", i)
	}
	f.Close()

	lines := readLogTailSince(logPath, offset, 3)
	want := []string{"new line 3", "new line 4", "new line 5"}
	if len(lines) != len(want) {
		t.Fatalf("readLogTailSince = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("readLogTailSince[%d] = %q, want %q", i, lines[i], want[i])
		}
	}

	if got := readLogTailSince(logPath, 1<<40, 3); len(got) != 0 {
		t.Fatalf("readLogTailSince(past EOF) = %q, want empty", got)
	}
}

// TestDaemonStartupFailureError pins the friendly classification of a daemon
// child that died during startup: the two failure modes users actually hit
// (token rejected, server unreachable) get a one-line reason plus an
// actionable next step instead of a raw log dump; only unrecognized failures
// fall back to a log excerpt, and even that excerpt drops DBG/INF noise.
// Classification reads both sinks: structured slog lines land in daemon.log,
// while the child's final cobra "Error: ..." line and panics land in the
// crash sink (daemon.err.log).
func TestDaemonStartupFailureError(t *testing.T) {
	t.Parallel()

	writeLog := func(t *testing.T, name, content string) string {
		t.Helper()
		logPath := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
		return logPath
	}

	t.Run("token rejected", func(t *testing.T) {
		t.Parallel()
		logPath := writeLog(t, "daemon.log", `18:29:58.416 INF authenticated component=daemon
18:29:58.425 WRN auth token rejected by server — run 'multica login' to re-authenticate component=daemon error="POST /api/tokens/current/renew returned 401: {\"error\":\"invalid token\"}"
list workspaces: GET /api/workspaces returned 401: {"error":"invalid token"}
`)
		err := daemonStartupFailureError(daemonStartupLogs{logPath: logPath}, nil, "", "http://localhost:8080")
		msg := err.Error()
		if !strings.Contains(msg, "rejected your login token") || !strings.Contains(msg, "multica login") {
			t.Fatalf("error = %q, want token-rejected reason with login hint", msg)
		}
		if strings.Contains(msg, "component=daemon") {
			t.Fatalf("error = %q, want no raw log lines for a classified failure", msg)
		}
	})

	t.Run("token rejected with profile", func(t *testing.T) {
		t.Parallel()
		logPath := writeLog(t, "daemon.log", "WRN auth token rejected by server error=\"returned 401\"\n")
		err := daemonStartupFailureError(daemonStartupLogs{logPath: logPath}, nil, "staging", "http://localhost:8080")
		if !strings.Contains(err.Error(), "multica login --profile staging") {
			t.Fatalf("error = %q, want profile-scoped login hint", err)
		}
	})

	t.Run("server unreachable via crash sink", func(t *testing.T) {
		t.Parallel()
		// The detached child routes slog into daemon.log, but its final cobra
		// error line goes to raw stderr — the crash sink. The "connection
		// refused" reason may therefore exist ONLY there.
		logPath := writeLog(t, "daemon.log", `18:40:46.588 DBG token renewal failed; will retry on next cycle component=daemon error="Post \"http://localhost:8080/api/tokens/current/renew\": dial tcp [::1]:8080: connect: connection refused"
`)
		errLogPath := writeLog(t, "daemon.err.log", `Error: list workspaces: Get "http://localhost:8080/api/workspaces": dial tcp [::1]:8080: connect: connection refused
`)
		err := daemonStartupFailureError(daemonStartupLogs{logPath: logPath, errLogPath: errLogPath}, nil, "", "http://localhost:8080")
		msg := err.Error()
		if !strings.Contains(msg, "cannot reach the Multica server at http://localhost:8080") {
			t.Fatalf("error = %q, want server-unreachable reason with URL", msg)
		}
		if strings.Contains(msg, "dial tcp") || strings.Contains(msg, "component=daemon") {
			t.Fatalf("error = %q, want no raw log lines for a classified failure", msg)
		}
	})

	t.Run("unrecognized failure keeps filtered excerpt plus crash output", func(t *testing.T) {
		t.Parallel()
		logPath := writeLog(t, "daemon.log", `18:00:00.001 DBG some debug detail component=daemon
18:00:00.002 INF starting daemon component=daemon
18:00:00.003 ERR something exploded component=daemon
`)
		errLogPath := writeLog(t, "daemon.err.log", "open /nope: permission denied\n")
		err := daemonStartupFailureError(daemonStartupLogs{logPath: logPath, errLogPath: errLogPath}, nil, "", "http://localhost:8080")
		msg := err.Error()
		if !strings.Contains(msg, "something exploded") || !strings.Contains(msg, "permission denied") {
			t.Fatalf("error = %q, want WRN/ERR and crash lines kept", msg)
		}
		if strings.Contains(msg, "some debug detail") || strings.Contains(msg, "starting daemon") {
			t.Fatalf("error = %q, want DBG/INF noise dropped", msg)
		}
		if !strings.Contains(msg, errLogPath) {
			t.Fatalf("error = %q, want pointer to the crash sink", msg)
		}
	})

	t.Run("empty logs", func(t *testing.T) {
		t.Parallel()
		logPath := writeLog(t, "daemon.log", "")
		err := daemonStartupFailureError(daemonStartupLogs{logPath: logPath}, nil, "", "")
		if !strings.Contains(err.Error(), "daemon exited during startup") || !strings.Contains(err.Error(), logPath) {
			t.Fatalf("error = %q, want generic failure pointing at the log file", err)
		}
	})
}

// TestDaemonStartBackgroundReportsEarlyChildExit pins the fail-fast contract
// for an authenticated `daemon start` whose child dies during preflight
// (unreachable server, token rejected with 401, ...): the parent must notice
// the exit and report failure immediately instead of polling the health port
// for the full 45s readiness window and ending with a vague "check logs"
// warning and exit code 0.
//
// The spawned child is stubbed to `false` via daemonExecutable so it dies
// immediately with a non-zero status, the same shape as a failed preflight.
func TestDaemonStartBackgroundReportsEarlyChildExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("false binary unavailable: %v", err)
	}
	orig := daemonExecutable
	daemonExecutable = func() (string, error) { return falseBin, nil }
	t.Cleanup(func() { daemonExecutable = orig })

	const profile = "child-exit-test"
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_fake"}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().Bool("foreground", false, "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	if err := cmd.Flags().Set("profile", profile); err != nil {
		t.Fatalf("set profile flag: %v", err)
	}

	start := time.Now()
	err = runDaemonStart(cmd, nil)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "daemon exited during startup") {
		t.Fatalf("runDaemonStart() = %v, want startup failure error", err)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("runDaemonStart took %s, want early-exit detection well before the 45s readiness window", elapsed)
	}
}

// TestDaemonRestartUnauthenticatedFailsBeforeStopping pins the ordering that
// makes `daemon restart` safe when the user is not logged in: the auth check
// must run BEFORE the stop phase. Otherwise restart kills the running daemon
// and only then discovers it cannot start a replacement, leaving the user
// with no daemon at all.
func TestDaemonRestartUnauthenticatedFailsBeforeStopping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const profile = "restart-authtest"

	// Fake running daemon on the profile's health port. Any shutdown attempt
	// means restart touched the daemon before checking auth.
	stopped := fakeRunningDaemon(t, profile)

	err := runDaemonRestart(newRestartTestCmd(t, profile), nil)
	if err == nil || !strings.Contains(err.Error(), "multica login --profile restart-authtest") {
		t.Fatalf("runDaemonRestart() = %v, want not-logged-in error with login hint", err)
	}
	select {
	case <-stopped:
		t.Fatal("restart asked the running daemon to shut down before the auth check")
	default:
	}
}

// fakeRunningDaemon serves a fake healthy daemon on the given profile's health
// port and reports any /shutdown request on the returned channel. The PID in
// /health is our own so a kill-fallback would be visible as a test crash too.
func fakeRunningDaemon(t *testing.T, profile string) <-chan struct{} {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", healthPortForProfile(profile)))
	if err != nil {
		t.Skipf("health port for profile %s unavailable: %v", profile, err)
	}
	stopped := make(chan struct{}, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"running","pid":%d}`, os.Getpid())))
		case "/shutdown":
			select {
			case stopped <- struct{}{}:
			default:
			}
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return stopped
}

func newRestartTestCmd(t *testing.T, profile string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "restart"}
	cmd.Flags().Bool("foreground", false, "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	if err := cmd.Flags().Set("profile", profile); err != nil {
		t.Fatalf("set profile flag: %v", err)
	}
	return cmd
}

// TestDaemonRestartRejectedTokenFailsBeforeStopping pins the restart safety
// guarantee beyond the empty-token case: a stored token that the server
// rejects with 401 (expired or revoked) must abort the restart BEFORE the
// running daemon is stopped. Otherwise restart kills the working daemon and
// the replacement child dies in preflight, leaving no daemon at all (#5165).
func TestDaemonRestartRejectedTokenFailsBeforeStopping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")

	const profile = "restart-401test"

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_revoked", ServerURL: api.URL}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	stopped := fakeRunningDaemon(t, profile)

	err := runDaemonRestart(newRestartTestCmd(t, profile), nil)
	if err == nil || !strings.Contains(err.Error(), "rejected your login token") {
		t.Fatalf("runDaemonRestart() = %v, want token-rejected error", err)
	}
	if !strings.Contains(err.Error(), "multica login --profile restart-401test") {
		t.Fatalf("runDaemonRestart() = %v, want profile login hint", err)
	}
	select {
	case <-stopped:
		t.Fatal("restart asked the running daemon to shut down despite a rejected token")
	default:
	}
}

// TestDaemonRestartUnreachableServerFailsBeforeStopping pins the other half of
// the restart preflight: when the configured server cannot be reached at all,
// restart must abort before stopping the running daemon, because the
// replacement child would die in preflight against the same dead server.
func TestDaemonRestartUnreachableServerFailsBeforeStopping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")

	const profile = "restart-unreachable-test"

	// Grab a port that is guaranteed closed: listen, record, close.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadURL := "http://" + ln.Addr().String()
	ln.Close()

	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_fake", ServerURL: deadURL}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	stopped := fakeRunningDaemon(t, profile)

	err = runDaemonRestart(newRestartTestCmd(t, profile), nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reach the Multica server") {
		t.Fatalf("runDaemonRestart() = %v, want server-unreachable error", err)
	}
	select {
	case <-stopped:
		t.Fatal("restart asked the running daemon to shut down despite an unreachable server")
	default:
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
