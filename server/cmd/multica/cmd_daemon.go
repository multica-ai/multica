package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	logger_pkg "github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/selfexec"
	"github.com/multica-ai/multica/server/internal/util"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Control the local agent runtime daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local agent runtime daemon",
	Long:  "Start the daemon process that polls for tasks and executes them using local agent CLIs (Claude, Codex).\nRuns in the background by default. Use --foreground to run in the current terminal.",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the running daemon (stop + start)",
	RunE:  runDaemonRestart,
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show daemon logs",
	RunE:  runDaemonLogs,
}

var daemonDiskUsageCmd = &cobra.Command{
	Use:   "disk-usage",
	Short: "Show daemon workspace disk usage by task or workspace",
	Long: "Walks the daemon's workspaces root and reports per-task or per-workspace disk usage.\n" +
		"Default view is per-task, sorted by size descending. --by-workspace switches to a per-workspace summary;\n" +
		"--top N keeps only the largest N entries.\n\n" +
		"By default only the current profile's root is scanned. --all-profiles aggregates across every workspace\n" +
		"root — the default root plus each ~/.multica/profiles/* root, including the Desktop app's dedicated\n" +
		"`desktop-<host>` root — and prints a per-root breakdown with a combined grand total. In that mode --top\n" +
		"applies within each root and --workspaces-root is not allowed.\n\n" +
		"Bytes are split into total and the artifact-cleanable subset (node_modules, .next, .turbo by default,\n" +
		"overridable via MULTICA_GC_ARTIFACT_PATTERNS) so the report stays in sync with what the GC reclaims.\n" +
		"The walk skips .git and never follows symlinks. The daemon does not need to be running.",
	RunE: runDaemonDiskUsage,
}

func init() {
	f := daemonStartCmd.Flags()
	f.Bool("foreground", false, "Run in the foreground instead of background")
	f.Bool("takeover", false, "If another daemon already owns this machine, ask it to stop and take over instead of failing")
	f.String("daemon-id", "", "Unique daemon identifier (env: MULTICA_DAEMON_ID)")
	f.String("device-name", "", "Human-readable device name (env: MULTICA_DAEMON_DEVICE_NAME)")
	f.String("runtime-name", "", "Runtime display name (env: MULTICA_AGENT_RUNTIME_NAME)")
	f.Duration("poll-interval", 0, "Task poll interval (env: MULTICA_DAEMON_POLL_INTERVAL)")
	f.Duration("heartbeat-interval", 0, "Heartbeat interval (env: MULTICA_DAEMON_HEARTBEAT_INTERVAL)")
	f.Duration("agent-timeout", 0, "Absolute per-task wall-clock cap; 0 = no cap, rely on the watchdogs (env: MULTICA_AGENT_TIMEOUT)")
	f.Duration("codex-semantic-inactivity-timeout", 0, "Codex semantic inactivity timeout (env: MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT)")
	f.Duration("codex-handshake-timeout", 0, "Codex app-server startup RPC timeout (env: MULTICA_CODEX_HANDSHAKE_TIMEOUT)")
	f.Int("max-concurrent-tasks", 0, "Max tasks running in parallel (env: MULTICA_DAEMON_MAX_CONCURRENT_TASKS)")
	f.Bool("no-auto-update", false, "Disable periodic CLI self-update (env: MULTICA_DAEMON_AUTO_UPDATE=false)")
	f.Duration("auto-update-interval", 0, "How often to poll GitHub for a newer release (env: MULTICA_DAEMON_AUTO_UPDATE_INTERVAL)")

	daemonLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	daemonLogsCmd.Flags().IntP("lines", "n", 50, "Number of lines to show")

	daemonStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// restart shares all the same flags as start
	rf := daemonRestartCmd.Flags()
	rf.Bool("foreground", false, "Run in the foreground instead of background")
	rf.Bool("takeover", false, "If another daemon already owns this machine, ask it to stop and take over instead of failing")
	rf.String("daemon-id", "", "Unique daemon identifier (env: MULTICA_DAEMON_ID)")
	rf.String("device-name", "", "Human-readable device name (env: MULTICA_DAEMON_DEVICE_NAME)")
	rf.String("runtime-name", "", "Runtime display name (env: MULTICA_AGENT_RUNTIME_NAME)")
	rf.Duration("poll-interval", 0, "Task poll interval (env: MULTICA_DAEMON_POLL_INTERVAL)")
	rf.Duration("heartbeat-interval", 0, "Heartbeat interval (env: MULTICA_DAEMON_HEARTBEAT_INTERVAL)")
	rf.Duration("agent-timeout", 0, "Absolute per-task wall-clock cap; 0 = no cap, rely on the watchdogs (env: MULTICA_AGENT_TIMEOUT)")
	rf.Duration("codex-semantic-inactivity-timeout", 0, "Codex semantic inactivity timeout (env: MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT)")
	rf.Duration("codex-handshake-timeout", 0, "Codex app-server startup RPC timeout (env: MULTICA_CODEX_HANDSHAKE_TIMEOUT)")
	rf.Int("max-concurrent-tasks", 0, "Max tasks running in parallel (env: MULTICA_DAEMON_MAX_CONCURRENT_TASKS)")
	rf.Bool("no-auto-update", false, "Disable periodic CLI self-update (env: MULTICA_DAEMON_AUTO_UPDATE=false)")
	rf.Duration("auto-update-interval", 0, "How often to poll GitHub for a newer release (env: MULTICA_DAEMON_AUTO_UPDATE_INTERVAL)")

	df := daemonDiskUsageCmd.Flags()
	df.Bool("by-workspace", false, "Aggregate output by workspace instead of by task")
	df.Bool("by-task", false, "Per-task view (default; mutually exclusive with --by-workspace)")
	df.Int("top", 0, "Keep only the largest N entries (per root in --all-profiles mode)")
	df.String("output", "table", "Output format: table or json")
	df.String("workspaces-root", "", "Override the workspaces root path (default: same as the daemon)")
	df.Bool("all-profiles", false, "Scan every workspace root (default root + all ~/.multica/profiles/* roots, incl. the Desktop app's) and report a combined total")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	daemonCmd.AddCommand(daemonDiskUsageCmd)
}

// daemonDirForProfile returns the state directory for the given profile.
// Empty profile → ~/.multica/, named profile → ~/.multica/profiles/<name>/.
func daemonDirForProfile(profile string) string {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return ""
	}
	return dir
}

func daemonPIDPathForProfile(profile string) string {
	return filepath.Join(daemonDirForProfile(profile), "daemon.pid")
}

func daemonLogPathForProfile(profile string) string {
	return filepath.Join(daemonDirForProfile(profile), "daemon.log")
}

// daemonStderrLogPathForProfile is the sink for the foreground daemon's raw
// stdout/stderr — Go runtime panics, fatal aborts, and any pre-logger output.
// It is deliberately separate from daemon.log: the foreground daemon owns
// daemon.log through a rotating writer (newDaemonLogRotator), and letting the
// child also hold daemon.log open via inherited fds would block rotation's
// rename on Windows. Because every structured log line already flows through
// slog into the rotating daemon.log, this file only receives rare crash output
// and stays near-empty for a healthy daemon.
func daemonStderrLogPathForProfile(profile string) string {
	return filepath.Join(daemonDirForProfile(profile), "daemon.err.log")
}

// Daemon log rotation policy. Defaults keep the active daemon.log small enough
// to open in an editor while retaining recent history; each is overridable via
// env so operators can tune retention without a rebuild. Whichever of backups
// / age is hit first prunes a rotated file.
const (
	defaultDaemonLogMaxSizeMB  = 20 // rotate the active daemon.log once it reaches this size
	defaultDaemonLogMaxBackups = 5  // how many rotated files to keep
	defaultDaemonLogMaxAgeDays = 30 // drop rotated files older than this
	// errLogMaxBytes bounds daemon.err.log (raw crash/panic sink). It is
	// enforced by rolling the file to a single ".1" backup at open time, so a
	// crash loop that repeatedly appends panic output can't reintroduce the
	// unbounded-growth problem on a different file.
	errLogMaxBytes = 5 * 1024 * 1024
)

// newDaemonLogRotator builds the size-based rotating writer backing daemon.log.
// Rotated files are gzip-compressed to keep the on-disk footprint small. The
// bulk of daemon.log volume is the daemon's own slog output plus agent
// subprocess stderr (all forwarded through slog), so bounding this writer
// bounds essentially all growth.
func newDaemonLogRotator(logPath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    envPositiveIntOrDefault("MULTICA_DAEMON_LOG_MAX_SIZE_MB", defaultDaemonLogMaxSizeMB),
		MaxBackups: envPositiveIntOrDefault("MULTICA_DAEMON_LOG_MAX_BACKUPS", defaultDaemonLogMaxBackups),
		MaxAge:     envPositiveIntOrDefault("MULTICA_DAEMON_LOG_MAX_AGE_DAYS", defaultDaemonLogMaxAgeDays),
		Compress:   true,
	}
}

// envPositiveIntOrDefault reads a strictly positive integer env var, falling
// back to def when unset, blank, malformed, zero, or negative. Zero is treated
// as "use default" on purpose: lumberjack reads MaxSize=0 as 100MB and
// MaxBackups=0 / MaxAge=0 as "keep everything", so honoring a literal 0 would
// silently disable retention and reintroduce unbounded growth.
func envPositiveIntOrDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// openBoundedErrLog rolls daemon.err.log to a single ".1" backup when it has
// grown past errLogMaxBytes, then opens it for appending. This keeps the raw
// crash sink bounded across restarts (see errLogMaxBytes) without pulling in a
// second rotating writer — the file is only ever written by inherited fds.
func openBoundedErrLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() >= errLogMaxBytes {
		// Best-effort roll; ignore errors and fall through to append so a
		// rename failure never blocks daemon startup.
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// healthPortForProfile returns the health check port for the given profile.
// Default profile uses the standard port (19514). Named profiles get a
// deterministic offset derived from the profile name.
func healthPortForProfile(profile string) int {
	if profile == "" {
		return daemon.DefaultHealthPort
	}
	// Simple hash: sum of bytes mod 1000, offset from base+1.
	var h int
	for _, b := range []byte(profile) {
		h += int(b)
	}
	return daemon.DefaultHealthPort + 1 + (h % 1000)
}

// prepareDaemonOwnership makes sure it is safe to (re)start a daemon on this
// machine before one is spawned. At most one daemon may own the machine-global
// lock at cli.ProfileDir("")/daemon.lock, which spans every profile — so it
// catches a Desktop-spawned daemon running under a different profile that the
// per-profile health-port guard cannot see (VWO-365).
//
// It runs in the LAUNCHER purely to give a fast, actionable error (or perform a
// requested takeover); the spawned daemon's Run re-acquires the lock for real.
//
//   - break-glass (MULTICA_DAEMON_ALLOW_MULTIPLE set) → skip.
//   - lock free → nil.
//   - lock held, --takeover → ask the incumbent to stop, wait for release.
//   - lock held, no --takeover → the actionable conflict error.
//
// ownershipHandoffWait bounds how long the launcher waits for a shutting-down
// incumbent to release the machine lock. It covers the gap between a daemon's
// health port closing (at the start of shutdown) and its ownership lock
// releasing at the very end, so a `daemon restart`, a stop-then-start, or a
// --takeover does not spuriously report a conflict/timeout mid-shutdown.
// Sized to the full graceful-shutdown bound: pollLoop waits up to 30s for
// in-flight tasks, then deregisterRuntimes has a 5s timeout — 45s adds margin.
const ownershipHandoffWait = 45 * time.Second

func prepareDaemonOwnership(cmd *cobra.Command) error {
	if daemon.OwnershipBypassed() {
		return nil
	}
	baseDir, err := cli.ProfileDir("")
	if err != nil {
		return fmt.Errorf("resolve base config dir for daemon ownership lock: %w", err)
	}
	free, incumbent, ok, err := daemon.ProbeOwnership(baseDir)
	if err != nil {
		return err
	}
	if !free {
		if takeover, _ := cmd.Flags().GetBool("takeover"); takeover {
			if err := takeoverDaemonOwner(baseDir, incumbent); err != nil {
				return err
			}
		} else if ok && incumbent.HealthPort > 0 && daemonAliveOnPort(incumbent.HealthPort) {
			// The lock is held and the incumbent is genuinely live: reject
			// immediately with an actionable error.
			return ownershipConflictErr(baseDir, incumbent, ok)
		} else if !waitForOwnershipFree(baseDir, ownershipHandoffWait) {
			// Not answering health means it is shutting down — the daemon a
			// `restart`/`stop` just asked to exit, still draining tasks and
			// deregistering before it releases the lock — so wait a bounded
			// time for the clean handoff rather than reject a legitimate
			// restart. A wedged daemon that never releases lands here.
			return ownershipConflictErr(baseDir, incumbent, ok)
		}
	}
	// The lock is (now) free. A daemon can still be running WITHOUT it: one
	// from a release predating single-daemon ownership, or one started under
	// the break-glass env. Starting alongside it would recreate the exact
	// two-writer hazard the lock exists to prevent, so sweep every known
	// profile's health port and refuse while such a daemon is alive.
	return detectUnlockedDaemon(baseDir)
}

// detectUnlockedDaemon scans every known profile's health port for a daemon
// running WITHOUT holding the machine lock. Only reachable while the lock is
// free — and a lock-aware daemon acquires the lock BEFORE it binds its health
// port — so an alive health endpoint here can only be a daemon from a pre-lock
// release or one bypassing the guard. This closes the rolling-upgrade blind
// spot: without it, upgrading the CLI while a legacy daemon runs under another
// profile (e.g. the Desktop's desktop-<host> profile) would let the new daemon
// start alongside it. Ports are derived from the profile directories that
// actually exist plus the default profile, so the sweep is a handful of
// fail-fast local probes, not a port scan.
func detectUnlockedDaemon(baseDir string) error {
	if daemon.OwnershipBypassed() {
		return nil
	}
	ports := map[int]string{daemon.DefaultHealthPort: ""} // port -> profile ("" = default)
	if entries, err := os.ReadDir(filepath.Join(baseDir, "profiles")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				ports[healthPortForProfile(e.Name())] = e.Name()
			}
		}
	}
	for port, profile := range ports {
		health := probeDaemonHealth(port)
		if !daemonAlive(health) {
			continue
		}
		// A daemon visible on localhost but running in an environment this
		// launcher can't manage — e.g. a Linux daemon inside WSL2 reachable
		// through Windows localhost forwarding (#3916) — is not sharing this
		// home's checkouts the way a native daemon does, and its lifecycle is
		// not ours to gate on. Skip it rather than refuse with a stop hint the
		// user can't act on. A daemon that reports no OS (older release) is
		// treated as native: fail safe toward refusing.
		if hostOS, _ := health["os"].(string); hostOS != "" && hostOS != runtime.GOOS {
			continue
		}
		label := "the default profile"
		stopHint := "`multica daemon stop`"
		if profile != "" {
			label = fmt.Sprintf("profile %q", profile)
			stopHint = fmt.Sprintf("`multica --profile %s daemon stop`", profile)
		}
		return fmt.Errorf(
			"a daemon that does not hold the machine ownership lock is running for %s (health port %d) — likely an older Multica release from before single-daemon ownership; stop it with %s and retry, or set MULTICA_DAEMON_ALLOW_MULTIPLE=1 to bypass the guard at your own risk",
			label, port, stopHint,
		)
	}
	return nil
}

func ownershipConflictErr(baseDir string, incumbent daemon.OwnerInfo, hasInfo bool) error {
	return &daemon.OwnershipConflict{Path: daemon.OwnershipLockPath(baseDir), Incumbent: incumbent, HasInfo: hasInfo}
}

// daemonAliveOnPort reports whether a daemon health endpoint answers on port.
func daemonAliveOnPort(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return daemonAlive(checkDaemonHealthOnPort(ctx, port))
}

// probeDaemonHealth is an indirection over checkDaemonHealthOnPort so tests can
// stub port liveness instead of binding real well-known ports (which would
// collide with an actual daemon on a developer machine). Returns the raw
// health map; a dead port yields a map daemonAlive() rejects.
var probeDaemonHealth = func(port int) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return checkDaemonHealthOnPort(ctx, port)
}

// waitForOwnershipFree polls until the machine lock is free or timeout elapses.
func waitForOwnershipFree(baseDir string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if free, _, _, err := daemon.ProbeOwnership(baseDir); err == nil && free {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// takeoverDaemonOwner asks the incumbent daemon to shut down (via its recorded
// health port — cross-platform, no OS signals) and waits until it releases the
// machine lock. The OS drops the incumbent's advisory lock the instant its
// process exits, so a successful shutdown frees the lock with no stale window.
func takeoverDaemonOwner(baseDir string, incumbent daemon.OwnerInfo) error {
	if incumbent.HealthPort <= 0 {
		return fmt.Errorf("cannot take over: owner health port unknown (owner pid %d); stop it manually with `multica daemon stop`", incumbent.PID)
	}
	fmt.Fprintf(os.Stderr, "Taking over from daemon pid %d (profile %q) on health port %d...\n",
		incumbent.PID, incumbent.Profile, incumbent.HealthPort)
	if err := requestDaemonShutdown(incumbent.HealthPort); err != nil {
		// A refused connection here is EXPECTED when the incumbent is already
		// shutting down: it closes its health listener at the start of shutdown
		// while holding the lock through the 30s task drain + deregister. Never
		// force-kill on this path — SIGKILL would skip the drain and the
		// deregister, and orphaned agent subprocesses could still be writing a
		// checkout when the new owner starts dispatching. Fall through to the
		// bounded wait; a genuinely wedged incumbent fails loudly below with a
		// manual remedy instead of being forced.
		fmt.Fprintf(os.Stderr, "Shutdown request not delivered (%v); waiting for the daemon to release the machine on its own...\n", err)
	}
	if waitForOwnershipFree(baseDir, ownershipHandoffWait) {
		fmt.Fprintln(os.Stderr, "Previous daemon released the machine; continuing.")
		return nil
	}
	return fmt.Errorf("takeover timed out: daemon pid %d did not release the machine lock within %s (it may still be draining an in-flight task); retry, or if it is truly stuck stop it manually (kill %d) and start again", incumbent.PID, ownershipHandoffWait, incumbent.PID)
}

// --- daemon start ---

func runDaemonStart(cmd *cobra.Command, _ []string) error {
	foreground, _ := cmd.Flags().GetBool("foreground")
	if foreground {
		return runDaemonForeground(cmd)
	}
	return runDaemonBackground(cmd)
}

func runDaemonBackground(cmd *cobra.Command) error {
	profile := resolveProfile(cmd)
	healthPort := healthPortForProfile(profile)

	// Check if daemon is already running.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	if daemonAlive(health) {
		label := "daemon"
		if profile != "" {
			label = fmt.Sprintf("daemon [%s]", profile)
		}
		pid, _ := health["pid"].(float64)
		return fmt.Errorf("%s is already running (pid %v). Use 'daemon restart' to restart it", label, int(pid))
	}

	// Reject (or take over) a daemon owning this machine under ANY profile — the
	// cross-profile case the same-profile health check above misses (VWO-365).
	// Done before spawning so the user sees an actionable error immediately, not
	// the generic "may not have started" timeout after the child fails to lock.
	if err := prepareDaemonOwnership(cmd); err != nil {
		return err
	}

	// Resolve current executable so the foreground child reuses this binary.
	exePath, err := selfexec.Resolve()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Build child args: daemon start --foreground + forwarded flags.
	args := buildDaemonStartArgs(cmd)

	// Ensure daemon directory exists.
	dir := daemonDirForProfile(profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create daemon directory: %w", err)
	}

	// The child (foreground daemon) writes daemon.log itself through a
	// rotating writer; its inherited stdout/stderr only need to catch raw
	// crash output, which goes to a separate, non-rotated sink so it never
	// holds daemon.log open (see daemonStderrLogPathForProfile).
	errLogPath := daemonStderrLogPathForProfile(profile)
	logFile, err := openBoundedErrLog(errLogPath)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", errLogPath, err)
	}
	// Where the daemon's structured logs actually land (rotated), for the
	// user-facing hints below.
	logPath := daemonLogPathForProfile(profile)

	child := exec.Command(exePath, args...)
	child.Stdout = logFile
	child.Stderr = logFile
	// On Windows we want to break the child out of the parent shell's Job
	// Object so the daemon survives parent-shell exit. If the parent's Job
	// has not granted BREAKAWAY_OK, CreateProcess returns
	// ERROR_ACCESS_DENIED — fall back to spawning without breakaway, which
	// matches the pre-fix behaviour. On Unix the bool is a no-op.
	child.SysProcAttr = daemonSysProcAttr(true)

	if err := child.Start(); err != nil {
		if isAccessDeniedSpawnErr(err) {
			// Retry without breakaway. Reset the cmd state — exec.Cmd is
			// not safe to Start() twice, so build a fresh one.
			child = exec.Command(exePath, args...)
			child.Stdout = logFile
			child.Stderr = logFile
			child.SysProcAttr = daemonSysProcAttr(false)
			if err := child.Start(); err != nil {
				logFile.Close()
				return fmt.Errorf("start daemon (no breakaway): %w", err)
			}
		} else {
			logFile.Close()
			return fmt.Errorf("start daemon: %w", err)
		}
	}
	logFile.Close()
	pid := child.Process.Pid

	// Detach: we don't Wait() on the child — it runs independently.
	child.Process.Release()

	// Write PID file.
	pidPath := daemonPIDPathForProfile(profile)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write PID file: %v\n", err)
	}

	// Poll the health endpoint until the daemon reports ready ("running") or we
	// time out. The daemon binds the health port almost immediately but reports
	// status:"starting" until preflight finishes (PAT renew + initial workspace
	// sync, which exec's every configured agent for version detection and can
	// take ~20s on a cold cache). Wait long enough to cover that so a healthy
	// cold start is not misreported as a failure.
	const startupTimeout = 45 * time.Second
	deadline := time.Now().Add(startupTimeout)
	started := false
	lastStatus := ""
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		hctx, hcancel := context.WithTimeout(context.Background(), 2*time.Second)
		health = checkDaemonHealthOnPort(hctx, healthPort)
		hcancel()
		lastStatus, _ = health["status"].(string)
		if lastStatus == "running" {
			started = true
			break
		}
	}
	if !started {
		if lastStatus == "starting" {
			fmt.Fprintf(os.Stderr, "Daemon is still starting after %s (agent detection / workspace sync is taking longer than expected). Check logs:\n  %s\n", startupTimeout, logPath)
		} else {
			fmt.Fprintf(os.Stderr, "Daemon may not have started successfully. Check logs:\n  %s\n  %s (crash output)\n", logPath, errLogPath)
		}
		return nil
	}

	if profile != "" {
		fmt.Fprintf(os.Stderr, "Daemon [%s] started (pid %d, version %s)\n", profile, pid, version)
	} else {
		fmt.Fprintf(os.Stderr, "Daemon started (pid %d, version %s)\n", pid, version)
	}
	fmt.Fprintf(os.Stderr, "Logs: %s\n", logPath)
	return nil
}

// buildDaemonStartArgs constructs args for the background child process.
func buildDaemonStartArgs(cmd *cobra.Command) []string {
	args := []string{"daemon", "start", "--foreground"}

	if v := flagString(cmd, "daemon-id"); v != "" {
		args = append(args, "--daemon-id", v)
	}
	if v := flagString(cmd, "device-name"); v != "" {
		args = append(args, "--device-name", v)
	}
	if v := flagString(cmd, "runtime-name"); v != "" {
		args = append(args, "--runtime-name", v)
	}
	if d, _ := cmd.Flags().GetDuration("poll-interval"); d > 0 {
		args = append(args, "--poll-interval", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("heartbeat-interval"); d > 0 {
		args = append(args, "--heartbeat-interval", d.String())
	}
	// Forward agent-timeout when explicitly set, including an explicit 0
	// (= no cap), so it can override an environment MULTICA_AGENT_TIMEOUT.
	if cmd.Flags().Changed("agent-timeout") {
		d, _ := cmd.Flags().GetDuration("agent-timeout")
		args = append(args, "--agent-timeout", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("codex-semantic-inactivity-timeout"); d > 0 {
		args = append(args, "--codex-semantic-inactivity-timeout", d.String())
	}
	if d, _ := cmd.Flags().GetDuration("codex-handshake-timeout"); d > 0 {
		args = append(args, "--codex-handshake-timeout", d.String())
	}
	if n, _ := cmd.Flags().GetInt("max-concurrent-tasks"); n > 0 {
		args = append(args, "--max-concurrent-tasks", strconv.Itoa(n))
	}
	if b, _ := cmd.Flags().GetBool("no-auto-update"); b {
		args = append(args, "--no-auto-update")
	}
	if d, _ := cmd.Flags().GetDuration("auto-update-interval"); d > 0 {
		args = append(args, "--auto-update-interval", d.String())
	}

	// Forward global persistent flags.
	if v, _ := cmd.Flags().GetString("server-url"); v != "" {
		args = append(args, "--server-url", v)
	}
	if v := resolveProfile(cmd); v != "" {
		args = append(args, "--profile", v)
	}

	return args
}

func runDaemonForeground(cmd *cobra.Command) error {
	util.EnsureHiddenConsole()

	profile := resolveProfile(cmd)

	// Honor --takeover on a direct `daemon start --foreground --takeover`: free
	// the incumbent before Run tries to acquire the machine lock. Without
	// --takeover we skip the lock pre-check — Run's acquire is the sole
	// authority and returns the conflict error itself — so the common
	// background→foreground child path (which inherits no --takeover) does not
	// re-probe the lock.
	if takeover, _ := cmd.Flags().GetBool("takeover"); takeover {
		if err := prepareDaemonOwnership(cmd); err != nil {
			return err
		}
	} else if baseDir, err := cli.ProfileDir(""); err == nil {
		// The unlocked-daemon sweep, however, must run on EVERY launch path:
		// Run's lock acquire cannot see a pre-lock daemon (it holds no lock), so
		// without this the documented --foreground debugging path could start
		// alongside a legacy daemon during the rolling-upgrade window. Cheap —
		// a handful of fail-fast local probes — and a no-op re-check when this
		// process is the background launcher's child (the launcher swept
		// moments earlier). The takeover branch above already ends with the
		// same sweep inside prepareDaemonOwnership.
		if err := detectUnlockedDaemon(baseDir); err != nil {
			return err
		}
	}

	// Pick the log sink. A user who runs `daemon start --foreground` in a shell
	// keeps live, colored logging on their terminal (a documented debugging
	// path — see docs troubleshooting). A detached/background child, whose
	// stderr the launcher redirected to a file, instead routes structured logs
	// into the size-bounded, rotating daemon.log so the file can't grow without
	// limit. We distinguish the two by whether stderr is a terminal.
	var (
		logger     *slog.Logger
		logRotator *lumberjack.Logger
	)
	if logger_pkg.StderrIsTerminal() {
		logger = logger_pkg.NewLogger("daemon")
	} else {
		// An older self-update launcher may have handed this process daemon.log
		// itself as stdout/stderr. On Windows that inherited handle lacks
		// FILE_SHARE_DELETE and would block the rotator's rename, so re-point
		// our standard handles at the bounded crash sink first, leaving
		// daemon.log solely owned by the rotator. No-op on Unix, where an open
		// fd never blocks rename.
		repointStdioToErrLog(daemonStderrLogPathForProfile(profile))
		logRotator = newDaemonLogRotator(daemonLogPathForProfile(profile))
		defer logRotator.Close()
		// Route both this process's structured logger and the package-global
		// slog default through the rotator, so every log line — including
		// LoadConfig's slog.Warn below — is captured and rotated.
		logger = logger_pkg.NewWriterLoggerDefault("daemon", logRotator)
	}

	serverURL := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", "")
	if serverURL == "" {
		if c, err := cli.LoadCLIConfigForProfile(profile); err == nil && c.ServerURL != "" {
			serverURL = c.ServerURL
		}
	}
	overrides := daemon.Overrides{
		ServerURL:   serverURL,
		DaemonID:    flagString(cmd, "daemon-id"),
		DeviceName:  flagString(cmd, "device-name"),
		RuntimeName: flagString(cmd, "runtime-name"),
		Profile:     profile,
		HealthPort:  healthPortForProfile(profile),
	}
	if d, _ := cmd.Flags().GetDuration("poll-interval"); d > 0 {
		overrides.PollInterval = d
	}
	if d, _ := cmd.Flags().GetDuration("heartbeat-interval"); d > 0 {
		overrides.HeartbeatInterval = d
	}
	// Distinguish "flag not passed" from an explicit `--agent-timeout 0` so a
	// user can turn off an env-configured cap from the CLI.
	if cmd.Flags().Changed("agent-timeout") {
		d, _ := cmd.Flags().GetDuration("agent-timeout")
		overrides.AgentTimeout = &d
	}
	if d, _ := cmd.Flags().GetDuration("codex-semantic-inactivity-timeout"); d > 0 {
		overrides.CodexSemanticInactivityTimeout = d
	}
	if d, _ := cmd.Flags().GetDuration("codex-handshake-timeout"); d > 0 {
		overrides.CodexHandshakeTimeout = d
	}
	if n, _ := cmd.Flags().GetInt("max-concurrent-tasks"); n > 0 {
		overrides.MaxConcurrentTasks = n
	}
	if b, _ := cmd.Flags().GetBool("no-auto-update"); b {
		overrides.DisableAutoUpdate = true
	}
	if d, _ := cmd.Flags().GetDuration("auto-update-interval"); d > 0 {
		overrides.AutoUpdateCheckInterval = d
	}

	cfg, err := daemon.LoadConfig(overrides)
	if err != nil {
		return err
	}
	cfg.CLIVersion = version
	// Set by the Electron Desktop app when it spawns the CLI so the server
	// can mark those runtimes as "managed" and hide CLI self-update UI.
	cfg.LaunchedBy = os.Getenv("MULTICA_LAUNCHED_BY")

	ctx, stop := notifyShutdownContext(context.Background())
	defer stop()

	d := daemon.New(cfg, logger)

	// Write PID file so "daemon stop" can find us.
	if dir := daemonDirForProfile(profile); dir != "" {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(daemonPIDPathForProfile(profile), []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	defer os.Remove(daemonPIDPathForProfile(profile))

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	// Check if the daemon needs to restart after a CLI update.
	if restartBin := d.RestartBinary(); restartBin != "" {
		logger.Info("restarting daemon with updated binary", "path", restartBin)

		// The successor will open daemon.log through its own rotating writer,
		// and lumberjack does not support two writers managing one file. Stop
		// writing to our rotator before the successor starts: close it and move
		// this process's remaining handoff logs to the crash sink (stderr),
		// including the package-global slog default (which would otherwise
		// reopen daemon.log on its next write).
		if logRotator != nil {
			logger = logger_pkg.NewWriterLoggerDefault("daemon", os.Stderr)
			_ = logRotator.Close()
		}

		args := buildDaemonStartArgs(cmd)
		child := exec.Command(restartBin, args...)

		// The successor is a fresh foreground daemon that will open daemon.log
		// through its own rotating writer; hand it the raw-stderr sink so its
		// inherited fds don't hold daemon.log open (see runDaemonBackground).
		errLogPath := daemonStderrLogPathForProfile(profile)
		logFile, err := openBoundedErrLog(errLogPath)
		if err != nil {
			logger.Error("failed to open log file for restart", "error", err)
			// Runtimes were already deregistered by triggerRestart() before handoff.
			// The supervisor-spawned successor re-registers on startup; do not
			// duplicate cleanup here.
			return fmt.Errorf("failed to open daemon log file %s for restart: %w", errLogPath, err)
		}
		child.Stdout = logFile
		child.Stderr = logFile
		// Break out of the parent's Job Object on Windows; see the
		// runDaemonBackground call site for rationale.
		child.SysProcAttr = daemonSysProcAttr(true)

		if err := child.Start(); err != nil {
			// Runtimes were already deregistered by triggerRestart() before handoff.
			// The supervisor-spawned successor re-registers on startup; do not
			// duplicate cleanup here.
			if isAccessDeniedSpawnErr(err) {
				child = exec.Command(restartBin, args...)
				child.Stdout = logFile
				child.Stderr = logFile
				child.SysProcAttr = daemonSysProcAttr(false)
				if err := child.Start(); err != nil {
					logFile.Close()
					logger.Error("failed to start new daemon (no breakaway)", "error", err)
					return fmt.Errorf("failed to start new daemon at %s without breakaway: %w", restartBin, err)
				}
			} else {
				logFile.Close()
				logger.Error("failed to start new daemon", "error", err)
				return fmt.Errorf("failed to start new daemon at %s: %w", restartBin, err)
			}
		}
		logFile.Close()
		child.Process.Release()

		// Write new PID file.
		pidPath := daemonPIDPathForProfile(profile)
		os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0o644)

		logger.Info("new daemon started", "pid", child.Process.Pid)
	}

	return nil
}

// --- daemon restart ---

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	profile := resolveProfile(cmd)
	healthPort := healthPortForProfile(profile)

	// Stop if running.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	if daemonAlive(health) {
		pid, _ := health["pid"].(float64)
		if pid > 0 {
			fmt.Fprintf(os.Stderr, "Stopping daemon (pid %d)...\n", int(pid))
			if err := requestDaemonShutdown(healthPort); err != nil {
				if p, perr := os.FindProcess(int(pid)); perr == nil {
					_ = p.Kill()
				}
			}
			// Wait until the port is fully released (not merely past "running"),
			// otherwise the fresh start below races the old daemon's listener.
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				sctx, scancel := context.WithTimeout(context.Background(), 1*time.Second)
				h := checkDaemonHealthOnPort(sctx, healthPort)
				scancel()
				if !daemonAlive(h) {
					break
				}
			}
		}
	}

	// Start fresh.
	return runDaemonStart(cmd, args)
}

// --- daemon stop ---

func runDaemonStop(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	healthPort := healthPortForProfile(profile)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := checkDaemonHealthOnPort(ctx, healthPort)
	if !daemonAlive(health) {
		label := "Daemon"
		if profile != "" {
			label = fmt.Sprintf("Daemon [%s]", profile)
		}
		fmt.Fprintf(os.Stderr, "%s is not running.\n", label)
		return nil
	}

	pid, ok := health["pid"].(float64)
	if !ok || pid == 0 {
		return fmt.Errorf("could not determine daemon PID from health endpoint")
	}

	process, err := os.FindProcess(int(pid))
	if err != nil {
		return fmt.Errorf("find process %d: %w", int(pid), err)
	}

	// Request graceful shutdown via the daemon's HTTP /shutdown endpoint
	// rather than an OS signal. On Windows the daemon is spawned with
	// DETACHED_PROCESS so it shares no console with us, which means
	// GenerateConsoleCtrlEvent can't reach it; HTTP works on both
	// platforms and triggers the same context-cancel path the daemon
	// already uses for self-restart.
	if err := requestDaemonShutdown(healthPort); err != nil {
		fmt.Fprintf(os.Stderr, "Graceful shutdown request failed: %v — falling back to forced kill.\n", err)
		if kerr := process.Kill(); kerr != nil {
			return fmt.Errorf("kill daemon (pid %d): %w", int(pid), kerr)
		}
	}

	fmt.Fprintf(os.Stderr, "Stopping daemon (pid %d)...\n", int(pid))

	// Poll health endpoint until daemon is gone.
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
		h := checkDaemonHealthOnPort(ctx2, healthPort)
		cancel2()
		if !daemonAlive(h) {
			os.Remove(daemonPIDPathForProfile(profile))
			fmt.Fprintln(os.Stderr, "Daemon stopped.")
			return nil
		}
	}

	fmt.Fprintln(os.Stderr, "Daemon is still stopping. It may be finishing a running task.")
	return nil
}

// requestDaemonShutdown POSTs to the daemon's /shutdown endpoint to ask it
// to exit gracefully. Returns an error if the request could not be delivered
// (network error, non-2xx status, or the endpoint predates this change).
func requestDaemonShutdown(healthPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/shutdown", healthPort)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// --- daemon status ---

func runDaemonStatus(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	healthPort := healthPortForProfile(profile)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := checkDaemonHealthOnPort(ctx, healthPort)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, health)
	}

	label := "Daemon"
	if profile != "" {
		label = fmt.Sprintf("Daemon [%s]", profile)
	}

	switch health["status"] {
	case "running":
		printDaemonStatusReport(os.Stdout, label, health)
	case "starting":
		fmt.Fprintf(os.Stdout, "%s: starting (pid %v)\n", label, health["pid"])
	default:
		fmt.Fprintf(os.Stdout, "%s: stopped\n", label)
	}
	return nil
}

// printDaemonStatusReport renders a key/value summary of the daemon health
// response. The value column is aligned to the widest label so the dynamic
// "Daemon [profile]" row stays in step with the static rows below it.
func printDaemonStatusReport(w io.Writer, label string, health map[string]any) {
	type row struct{ key, value string }
	rows := []row{
		{label, fmt.Sprintf("running (pid %v, uptime %v)", health["pid"], health["uptime"])},
	}
	if version, ok := health["cli_version"].(string); ok && version != "" {
		rows = append(rows, row{"Version", version})
	}
	if agents, ok := health["agents"].([]any); ok && len(agents) > 0 {
		parts := make([]string, len(agents))
		for i, a := range agents {
			parts[i] = fmt.Sprint(a)
		}
		rows = append(rows, row{"Agents", strings.Join(parts, ", ")})
	}
	if ws, ok := health["workspaces"].([]any); ok {
		rows = append(rows, row{"Workspaces", strconv.Itoa(len(ws))})
	}

	keyWidth := 0
	for _, r := range rows {
		if n := len(r.key); n > keyWidth {
			keyWidth = n
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%-*s  %s\n", keyWidth+1, r.key+":", r.value)
	}
}

// --- daemon logs ---

func runDaemonLogs(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	logPath := daemonLogPathForProfile(profile)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("no log file found at %s\nThe daemon may not have been started in background mode", logPath)
	}

	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")

	return tailLogFile(logPath, lines, follow)
}

// daemonAlive reports whether a health response indicates a live daemon
// process on the port — either fully "running" (ready) or still "starting"
// (port bound, preflight in progress). Lifecycle commands that only need to
// know "is a daemon there" (already-running guard, restart, stop) use this,
// whereas `daemon start`'s readiness wait gates on the stricter "running".
func daemonAlive(health map[string]any) bool {
	switch health["status"] {
	case "running", "starting":
		return true
	default:
		return false
	}
}

// checkDaemonHealthOnPort calls the daemon's local health endpoint on the given port.
func checkDaemonHealthOnPort(ctx context.Context, port int) map[string]any {
	addr := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return map[string]any{"status": "stopped"}
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return map[string]any{"status": "stopped"}
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]any{"status": "stopped"}
	}
	return result
}

// flagString returns a string flag value or empty string.
func flagString(cmd *cobra.Command, name string) string {
	val, _ := cmd.Flags().GetString(name)
	return val
}

// --- daemon disk-usage ---

func runDaemonDiskUsage(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	rootOverride, _ := cmd.Flags().GetString("workspaces-root")
	byWorkspace, _ := cmd.Flags().GetBool("by-workspace")
	byTask, _ := cmd.Flags().GetBool("by-task")
	top, _ := cmd.Flags().GetInt("top")
	output, _ := cmd.Flags().GetString("output")
	allProfiles, _ := cmd.Flags().GetBool("all-profiles")

	if byWorkspace && byTask {
		return fmt.Errorf("--by-workspace and --by-task are mutually exclusive")
	}
	if top < 0 {
		return fmt.Errorf("--top must be a non-negative integer")
	}
	if allProfiles && rootOverride != "" {
		return fmt.Errorf("--all-profiles and --workspaces-root are mutually exclusive")
	}

	if allProfiles {
		return runDaemonDiskUsageAggregate(byWorkspace, top, output)
	}

	workspacesRoot, err := daemon.ResolveWorkspacesRoot(profile, rootOverride)
	if err != nil {
		return fmt.Errorf("resolve workspaces root: %w", err)
	}

	report, err := daemon.ScanDiskUsage(workspacesRoot, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}

	if top > 0 {
		if byWorkspace {
			if top < len(report.Workspaces) {
				report.Workspaces = report.Workspaces[:top]
			}
		} else if top < len(report.Tasks) {
			report.Tasks = report.Tasks[:top]
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, report)
	}

	if byWorkspace {
		printDiskUsageWorkspaceTable(os.Stdout, report)
		printDiskUsageOtherRootsHint(os.Stdout, report, profile, rootOverride)
		return nil
	}
	printDiskUsageTaskTable(os.Stdout, report)
	printDiskUsageOtherRootsHint(os.Stdout, report, profile, rootOverride)
	return nil
}

// runDaemonDiskUsageAggregate scans every workspace root (the default root plus
// each ~/.multica/profiles/* root) and renders a per-root breakdown with a
// combined grand total. This is the path that surfaces the Desktop app's
// `desktop-<host>` root, which the default single-root scan never sees.
func runDaemonDiskUsageAggregate(byWorkspace bool, top int, output string) error {
	roots, err := enumerateDiskUsageRoots()
	if err != nil {
		return err
	}
	agg, err := daemon.ScanDiskUsageRoots(roots, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}

	// --top trims each root's table independently — the grand total in the
	// report stays anchored to the full scan, mirroring single-root --top.
	if top > 0 {
		for i := range agg.Roots {
			r := &agg.Roots[i].Report
			if byWorkspace {
				if top < len(r.Workspaces) {
					r.Workspaces = r.Workspaces[:top]
				}
			} else if top < len(r.Tasks) {
				r.Tasks = r.Tasks[:top]
			}
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, agg)
	}
	printAggregateDiskUsage(os.Stdout, agg, byWorkspace)
	return nil
}

// enumerateDiskUsageRoots returns the ordered, de-duplicated set of workspace
// roots to scan in --all-profiles mode: the default root first (always, for
// orientation even when empty), then each ~/.multica/profiles/* root that
// exists on disk, sorted by profile name. Roots that resolve to the same path
// (e.g. when MULTICA_WORKSPACES_ROOT pins every profile to one directory) are
// collapsed to a single entry.
func enumerateDiskUsageRoots() ([]daemon.DiskUsageRoot, error) {
	seen := map[string]bool{}
	out := make([]daemon.DiskUsageRoot, 0)

	if root, err := daemon.ResolveWorkspacesRoot("", ""); err == nil {
		out = append(out, daemon.DiskUsageRoot{Profile: "", Root: root})
		seen[root] = true
	}

	profilesRoot, err := profilesRootDir()
	if err != nil {
		return out, nil
	}
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		return out, nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		root, err := daemon.ResolveWorkspacesRoot(name, "")
		if err != nil || seen[root] {
			continue
		}
		// Skip profile roots that were never created on disk — a configured
		// profile whose daemon never ran has nothing to report.
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			continue
		}
		seen[root] = true
		out = append(out, daemon.DiskUsageRoot{Profile: name, Root: root})
	}
	return out, nil
}

func printAggregateDiskUsage(w io.Writer, agg daemon.AggregateDiskUsageReport, byWorkspace bool) {
	fmt.Fprintf(w, "Scanned %d workspace root(s).\n", len(agg.Roots))
	for _, root := range agg.Roots {
		fmt.Fprintln(w)
		label := "default"
		if root.Profile != "" {
			label = root.Profile
		}
		fmt.Fprintf(w, "[%s]\n", label)
		if byWorkspace {
			printDiskUsageWorkspaceTable(w, root.Report)
		} else {
			printDiskUsageTaskTable(w, root.Report)
		}
	}
	fmt.Fprintf(w, "\nGrand total: %s across %d task(s) in %d root(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(agg.TotalSizeBytes), agg.TotalTaskCount, len(agg.Roots),
		formatBytes(agg.TotalArtifactSizeBytes), agg.TotalArtifactRatio*100)
}

func printDiskUsageTaskTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalTaskCount == 0 {
		fmt.Fprintln(w, "(no task directories)")
		return
	}
	rows := make([][]string, 0, len(report.Tasks))
	var displayedSize, displayedArtifact int64
	for _, task := range report.Tasks {
		displayedSize += task.SizeBytes
		displayedArtifact += task.ArtifactSizeBytes
		rows = append(rows, []string{
			task.WorkspaceShort + "/" + task.TaskShort,
			task.Kind,
			emptyDash(task.ParentStatus),
			formatAge(task.AgeSeconds),
			formatBytes(task.SizeBytes),
			formatBytes(task.ArtifactSizeBytes),
		})
	}
	cli.PrintTable(w, []string{"PATH", "KIND", "STATUS", "AGE", "SIZE", "ARTIFACTS"}, rows)

	if len(report.Tasks) < report.TotalTaskCount {
		// Report-wide totals stay anchored to the full scan; the displayed
		// row is what the user is currently looking at. Calling these out
		// separately keeps `--top N` from misleading at-a-glance triage.
		fmt.Fprintf(w, "\nShowing top %d of %d task(s). Displayed: %s (%s artifacts). Scan total: %s (%s artifacts, %.1f%% reclaimable).\n",
			len(report.Tasks), report.TotalTaskCount,
			formatBytes(displayedSize), formatBytes(displayedArtifact),
			formatBytes(report.TotalSizeBytes), formatBytes(report.TotalArtifactSizeBytes),
			report.TotalArtifactRatio*100)
		return
	}
	fmt.Fprintf(w, "\nTotal: %s across %d task(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(report.TotalSizeBytes), report.TotalTaskCount,
		formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
}

func printDiskUsageWorkspaceTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalWorkspaceCount == 0 {
		fmt.Fprintln(w, "(no workspaces)")
		return
	}
	rows := make([][]string, 0, len(report.Workspaces))
	var displayedSize, displayedArtifact int64
	for _, ws := range report.Workspaces {
		displayedSize += ws.SizeBytes
		displayedArtifact += ws.ArtifactSizeBytes
		rows = append(rows, []string{
			ws.WorkspaceShort,
			strconv.Itoa(ws.TaskCount),
			formatBytes(ws.SizeBytes),
			formatBytes(ws.ArtifactSizeBytes),
			formatRatio(ws.ArtifactRatio),
			formatAge(ws.OldestAgeSeconds),
		})
	}
	cli.PrintTable(w, []string{"WORKSPACE", "TASKS", "SIZE", "ARTIFACTS", "ARTIFACT %", "OLDEST"}, rows)

	if len(report.Workspaces) < report.TotalWorkspaceCount {
		fmt.Fprintf(w, "\nShowing top %d of %d workspace(s). Displayed: %s (%s artifacts). Scan total: %s (%s artifacts, %.1f%% reclaimable).\n",
			len(report.Workspaces), report.TotalWorkspaceCount,
			formatBytes(displayedSize), formatBytes(displayedArtifact),
			formatBytes(report.TotalSizeBytes), formatBytes(report.TotalArtifactSizeBytes),
			report.TotalArtifactRatio*100)
		return
	}
	fmt.Fprintf(w, "\nTotal: %s across %d workspace(s); %s reclaimable as artifacts (%.1f%%).\n",
		formatBytes(report.TotalSizeBytes), report.TotalWorkspaceCount,
		formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
}

// printDiskUsageOtherRootsHint warns that workspace roots OTHER than the one
// just scanned also hold task directories — the case that hides the Desktop
// app's `desktop-<host>` root behind a non-empty default root. It fires
// whenever such roots exist (empty current root or not); the only opt-out is an
// explicit --workspaces-root, where the user already chose exactly what to scan.
func printDiskUsageOtherRootsHint(w io.Writer, report daemon.DiskUsageReport, profile, rootOverride string) {
	if rootOverride != "" {
		return
	}
	suggestions := diskUsageProfileSuggestions(profile, report.WorkspacesRoot)
	if len(suggestions) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Other workspace roots contain task directories:")
	for _, s := range suggestions {
		fmt.Fprintf(w, "  %s  # %s (%d task%s)\n",
			s.Command, s.Root, s.TaskCount, pluralS(s.TaskCount))
	}
	fmt.Fprintln(w, "Run 'multica daemon disk-usage --all-profiles' for a combined total across all roots.")
}

type diskUsageProfileSuggestion struct {
	Profile   string
	Command   string
	Root      string
	TaskCount int
}

func diskUsageProfileSuggestions(currentProfile, currentRoot string) []diskUsageProfileSuggestion {
	out := make([]diskUsageProfileSuggestion, 0)
	if currentProfile != "" {
		if root, err := daemon.ResolveWorkspacesRoot("", ""); err == nil && !samePath(root, currentRoot) {
			if taskCount := countDiskUsageTaskDirs(root); taskCount > 0 {
				out = append(out, diskUsageProfileSuggestion{
					Profile:   "",
					Command:   "multica daemon disk-usage",
					Root:      root,
					TaskCount: taskCount,
				})
			}
		}
	}

	profilesRoot, err := profilesRootDir()
	if err != nil {
		return out
	}
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profile := entry.Name()
		if profile == currentProfile {
			continue
		}
		root, err := daemon.ResolveWorkspacesRoot(profile, "")
		if err != nil || samePath(root, currentRoot) {
			continue
		}
		taskCount := countDiskUsageTaskDirs(root)
		if taskCount == 0 {
			continue
		}
		out = append(out, diskUsageProfileSuggestion{
			Profile:   profile,
			Command:   "multica --profile " + shellQuoteArg(profile) + " daemon disk-usage",
			Root:      root,
			TaskCount: taskCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskCount == out[j].TaskCount {
			return out[i].Profile < out[j].Profile
		}
		return out[i].TaskCount > out[j].TaskCount
	})
	const maxSuggestions = 5
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

func shellQuoteArg(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' ||
			r >= '0' && r <= '9' ||
			r >= 'A' && r <= 'Z' ||
			r >= 'a' && r <= 'z')
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func countDiskUsageTaskDirs(root string) int {
	wsEntries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		taskEntries, err := os.ReadDir(filepath.Join(root, wsEntry.Name()))
		if err != nil {
			continue
		}
		for _, taskEntry := range taskEntries {
			if taskEntry.IsDir() {
				count++
			}
		}
	}
	return count
}

func profilesRootDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".multica", "profiles"), nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// formatRatio renders a 0..1 fraction as a percentage to one decimal. A
// non-finite or negative input collapses to "0.0%" — total=0 workspaces
// shouldn't surface "NaN%".
func formatRatio(r float64) string {
	if r != r || r < 0 { // NaN check via inequality
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", r*100)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatBytes renders a byte count in IEC units (KiB/MiB/GiB) with one decimal
// place above 1 KiB. Kept intentionally compact so the table view stays
// scannable at terminal widths.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	prefix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), prefix)
}

// formatAge renders an age in the most human-friendly unit that still keeps
// the value above 1. "0s" stands for "less than a second" — matches what the
// GC log lines look like.
func formatAge(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	d := time.Duration(seconds) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int((d%(24*time.Hour))/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
