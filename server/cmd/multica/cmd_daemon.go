package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

var daemonProbeRuntimesCmd = &cobra.Command{
	Use:    "probe-runtimes",
	Short:  "Probe locally configured runtimes for the Desktop app",
	Hidden: true,
	RunE:   runDaemonProbeRuntimes,
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
		"A .git directory counts toward the total but never toward the artifact subset — the GC frees it only\n" +
		"when it removes the whole task directory. Symlinks are never followed.\n\n" +
		"The STATUS column shows the current status of the issue each directory belongs to, which requires\n" +
		"contacting the server. When the machine is offline, logged out, or the request fails, the column is\n" +
		"left blank and the rest of the report still prints. --by-workspace has no STATUS column and therefore\n" +
		"makes no network request unless --output json is also set. The daemon does not need to be running.",
	RunE: runDaemonDiskUsage,
}

func init() {
	f := daemonStartCmd.Flags()
	f.Bool("foreground", false, "Run in the foreground instead of background")
	f.String("daemon-id", "", "Unique daemon identifier (env: MULTICA_DAEMON_ID)")
	f.String("device-name", "", "Human-readable device name (env: MULTICA_DAEMON_DEVICE_NAME)")
	f.String("runtime-name", "", "Runtime display name (env: MULTICA_AGENT_RUNTIME_NAME)")
	f.String("workspaces-root", "", "Base directory for task workspaces (env: MULTICA_WORKSPACES_ROOT)")
	f.Duration("poll-interval", 0, "Task poll interval (env: MULTICA_DAEMON_POLL_INTERVAL)")
	f.Duration("heartbeat-interval", 0, "Heartbeat interval (env: MULTICA_DAEMON_HEARTBEAT_INTERVAL)")
	f.Duration("agent-timeout", 0, "Absolute per-task wall-clock cap; 0 = no cap, rely on the watchdogs (env: MULTICA_AGENT_TIMEOUT)")
	f.Duration("codex-semantic-inactivity-timeout", 0, "Codex semantic inactivity timeout (env: MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT)")
	f.Duration("codex-handshake-timeout", 0, "Codex app-server startup RPC timeout (env: MULTICA_CODEX_HANDSHAKE_TIMEOUT)")
	f.Int("max-concurrent-tasks", 0, "Max tasks running in parallel (env: MULTICA_DAEMON_MAX_CONCURRENT_TASKS)")
	f.Bool("no-auto-update", false, "Disable periodic CLI self-update (env: MULTICA_DAEMON_AUTO_UPDATE=false)")
	f.Duration("auto-update-interval", 0, "How often to poll GitHub for a newer release (env: MULTICA_DAEMON_AUTO_UPDATE_INTERVAL)")
	f.Bool("no-auto-reload", false, "Disable restarting when the multica binary on disk changes version (env: MULTICA_DAEMON_AUTO_RELOAD=false)")
	f.String("platform-context", "", "Multica context mode: full, minimal, or off (env: MULTICA_PLATFORM_CONTEXT)")
	f.Bool("no-platform-context", false, "Alias for --platform-context=off")

	daemonLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	daemonLogsCmd.Flags().IntP("lines", "n", 50, "Number of lines to show")

	daemonStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// restart shares all the same flags as start
	rf := daemonRestartCmd.Flags()
	rf.Bool("foreground", false, "Run in the foreground instead of background")
	rf.String("daemon-id", "", "Unique daemon identifier (env: MULTICA_DAEMON_ID)")
	rf.String("device-name", "", "Human-readable device name (env: MULTICA_DAEMON_DEVICE_NAME)")
	rf.String("runtime-name", "", "Runtime display name (env: MULTICA_AGENT_RUNTIME_NAME)")
	rf.String("workspaces-root", "", "Base directory for task workspaces (env: MULTICA_WORKSPACES_ROOT)")
	rf.Duration("poll-interval", 0, "Task poll interval (env: MULTICA_DAEMON_POLL_INTERVAL)")
	rf.Duration("heartbeat-interval", 0, "Heartbeat interval (env: MULTICA_DAEMON_HEARTBEAT_INTERVAL)")
	rf.Duration("agent-timeout", 0, "Absolute per-task wall-clock cap; 0 = no cap, rely on the watchdogs (env: MULTICA_AGENT_TIMEOUT)")
	rf.Duration("codex-semantic-inactivity-timeout", 0, "Codex semantic inactivity timeout (env: MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT)")
	rf.Duration("codex-handshake-timeout", 0, "Codex app-server startup RPC timeout (env: MULTICA_CODEX_HANDSHAKE_TIMEOUT)")
	rf.Int("max-concurrent-tasks", 0, "Max tasks running in parallel (env: MULTICA_DAEMON_MAX_CONCURRENT_TASKS)")
	rf.Bool("no-auto-update", false, "Disable periodic CLI self-update (env: MULTICA_DAEMON_AUTO_UPDATE=false)")
	rf.Duration("auto-update-interval", 0, "How often to poll GitHub for a newer release (env: MULTICA_DAEMON_AUTO_UPDATE_INTERVAL)")
	rf.Bool("no-auto-reload", false, "Disable restarting when the multica binary on disk changes version (env: MULTICA_DAEMON_AUTO_RELOAD=false)")
	rf.String("platform-context", "", "Multica context mode: full, minimal, or off (env: MULTICA_PLATFORM_CONTEXT)")
	rf.Bool("no-platform-context", false, "Alias for --platform-context=off")

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
	daemonCmd.AddCommand(daemonProbeRuntimesCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	daemonCmd.AddCommand(daemonDiskUsageCmd)
}

type daemonRuntimeProbe struct {
	ProbeResult     string         `json:"probe_result"`
	RuntimeCount    int            `json:"runtime_count"`
	ProviderSummary map[string]int `json:"provider_summary"`
}

func runDaemonProbeRuntimes(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon probe-runtimes"); err != nil {
		return err
	}
	cfg, err := daemon.LoadConfig(daemon.Overrides{
		Profile:       resolveProfile(cmd),
		AllowNoAgents: true,
	})
	if err != nil {
		return err
	}
	probe := daemonRuntimeProbeFromAgents(cfg.Agents)
	return json.NewEncoder(cmd.OutOrStdout()).Encode(probe)
}

func daemonRuntimeProbeFromAgents(agents map[string]daemon.AgentEntry) daemonRuntimeProbe {
	probe := daemonRuntimeProbe{
		ProbeResult:     "success",
		RuntimeCount:    len(agents),
		ProviderSummary: make(map[string]int, len(agents)),
	}
	for provider := range agents {
		probe.ProviderSummary[provider]++
	}
	return probe
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

// daemonProfileMismatchError reports that the daemon answering on a profile's
// health port is not that profile's daemon.
type daemonProfileMismatchError struct {
	Want string // profile the caller asked to act on
	Got  string // profile the daemon on that port reports; unset when Unreadable
	Port int
	// Unreadable marks a daemon that answered the profile field with something
	// that is not a string. It claimed an identity we cannot read, so it must
	// not be credited as ours — see daemonIdentityMismatch.
	Unreadable bool
}

func (e *daemonProfileMismatchError) Error() string {
	if e.Unreadable {
		return fmt.Sprintf(
			"port %d is serving a daemon that reported an unreadable profile identity\n"+
				"Refusing to act on it as %s: an identity that cannot be read cannot be confirmed to be yours.",
			e.Port, describeProfile(e.Want))
	}
	return fmt.Sprintf(
		"port %d is serving profile %s, not %s\n"+
			"Both profile names hash to the same health port, so this command would have acted on the wrong daemon.\n"+
			"Run it against %s directly, or rename one of the profiles.",
		e.Port, describeProfile(e.Got), describeProfile(e.Want), describeProfile(e.Got))
}

// statusNote is the one-line explanation `daemon status` prints under its
// verdict, where the command has not failed and there is no error to render.
func (e *daemonProfileMismatchError) statusNote() string {
	if e.Unreadable {
		return fmt.Sprintf("Note: port %d is serving a daemon whose profile identity could not be read.", e.Port)
	}
	return fmt.Sprintf("Note: port %d is serving %s, which hashes to the same port.",
		e.Port, describeProfile(e.Got))
}

// statusJSON is the additive `port_conflict` object for --output json.
func (e *daemonProfileMismatchError) statusJSON() map[string]any {
	if e.Unreadable {
		return map[string]any{"port": e.Port, "unreadable_identity": true}
	}
	return map[string]any{"port": e.Port, "profile": e.Got}
}

// describeProfile renders a profile name for humans, naming the default
// profile rather than printing an empty string.
func describeProfile(profile string) string {
	if profile == "" {
		return "the default profile"
	}
	return strconv.Quote(profile)
}

// daemonIdentityMismatch reports whether the daemon behind health belongs to a
// profile other than the one being acted on.
//
// The health port is a hash of the profile name into a 1000-port range, so
// distinct names collide — any rearrangement of the same characters lands on
// the same port. Without this check `--profile a daemon stop` reads the PID off
// whatever daemon answered and kills it, which for a collision is profile b's
// daemon and every runtime it hosts (#6694).
//
// A daemon that predates the profile field omits it entirely. That is the only
// case treated as "cannot prove identity, proceed anyway": refusing there would
// break lifecycle commands against a daemon that is merely older. Once the
// field is present it is enforced strictly — including the empty string, which
// is the default profile identifying itself, not a missing answer.
func daemonIdentityMismatch(health map[string]any, profile string, port int) error {
	raw, ok := health["profile"]
	if !ok {
		return nil
	}
	got, isString := raw.(string)
	if !isString {
		// Present but malformed (null, a number) is NOT the same as absent.
		// Dropping the type check would collapse such a value to "" and, for a
		// caller targeting the default profile, read as a match — handing the
		// lifecycle commands a daemon whose identity was never established.
		// Absence is a daemon too old to answer; this is a daemon answering
		// wrongly, so it fails closed.
		return &daemonProfileMismatchError{Want: profile, Port: port, Unreadable: true}
	}
	if got == profile {
		return nil
	}
	return &daemonProfileMismatchError{Want: profile, Got: got, Port: port}
}

// unknownProfileError reports an explicitly named --profile that has no
// directory under ~/.multica/profiles. It carries the known profile names so
// both the text and JSON renderings can list them without re-reading the disk.
type unknownProfileError struct {
	Profile string
	Known   []string
}

func (e *unknownProfileError) Error() string {
	if len(e.Known) == 0 {
		// The command is left unquoted so the shell-quoted profile name below
		// stays the only quoting in the line and the hint pastes cleanly.
		return fmt.Sprintf("unknown profile %q: no named profiles exist yet.\nCreate one with: multica login --profile %s", e.Profile, shellQuoteArg(e.Profile))
	}
	return fmt.Sprintf("unknown profile %q\nKnown profiles: %s", e.Profile, strings.Join(e.Known, ", "))
}

// jsonPayload renders the error for `--output json`. Known stays a non-nil
// slice so the field marshals as [] rather than null.
func (e *unknownProfileError) jsonPayload() map[string]any {
	known := e.Known
	if known == nil {
		known = []string{}
	}
	return map[string]any{
		"status":         "unknown_profile",
		"profile":        e.Profile,
		"known_profiles": known,
	}
}

// profileConfigFileName is the file whose presence marks a directory under the
// profiles root as an actual profile rather than just a parent of one. Kept in
// step with cli.CLIConfigPathForProfile.
const profileConfigFileName = "config.json"

// profileExists reports whether an explicitly named profile has state on disk.
//
// The name is resolved through the same path logic the config layer uses
// rather than matched against a flat directory listing, because a profile name
// may contain separators: `multica --profile team/dev config set ...` creates
// ~/.multica/profiles/team/dev. Matching against top-level entries would
// reject that profile while offering its parent "team" as a suggestion.
func profileExists(profile string) (bool, error) {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// knownProfiles lists the profiles that exist on disk, sorted by name, for use
// as suggestions after a name misses. A profile is a directory under the
// profiles root holding a config.json, at any depth — requiring that file is
// what keeps a bare parent directory such as "team" out of a list where every
// entry is meant to be directly usable as --profile.
//
// A missing profiles root is not an error: it just means no named profile has
// been created yet, the common case for a default-profile-only user.
func knownProfiles() ([]string, error) {
	root, err := profilesRootDir()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree costs us suggestions, not correctness:
			// skip it so the rest of the list still reaches the user.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Name() != profileConfigFileName {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil || rel == "." {
			return nil
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return nil, nil
		}
		return nil, walkErr
	}
	sort.Strings(names)
	return names, nil
}

// requireKnownProfile rejects an explicitly named profile that does not exist.
//
// healthPortForProfile hashes ANY string into a port, so without this check a
// mistyped --profile probes a port nothing is bound to and the command reports
// a flat "stopped" — indistinguishable from a daemon that is genuinely not
// running, even while the correctly-named daemon serves happily on another
// port (#6694). Failing loudly with the known names turns that dead end into a
// one-glance fix.
//
// The default profile is never validated: it owns ~/.multica directly, has no
// entry under profiles/, and is always legitimate.
//
// Callers must invoke this AFTER their daemon-managed-task guard
// (requireHumanLocalCommand, or daemonStatusHealthPort for status). Listing the
// profiles root inside a task would disclose the Owner's profile names, which
// those guards exist to prevent.
func requireKnownProfile(profile string) error {
	if profile == "" {
		return nil
	}
	exists, err := profileExists(profile)
	if err != nil {
		return fmt.Errorf("resolve profile %q: %w", profile, err)
	}
	if exists {
		return nil
	}
	// Only now is the listing worth its disk walk, and only now can it be
	// wrong in a way the user sees.
	names, err := knownProfiles()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}
	return &unknownProfileError{Profile: profile, Known: names}
}

// --- daemon start ---

// daemonExecutable resolves the binary to spawn as the background daemon
// child. Tests override it: spawning the resolved executable there would fork
// the test binary itself, which ignores the daemon args and re-runs the suite.
var daemonExecutable = selfexec.Resolve

// requireDaemonAuth fails fast when the user never ran `multica login`. The
// daemon child performs the same check (resolveAuth) and dies immediately,
// but the background parent can't see that exit — it would poll the health
// port for the full 45s readiness window and then print a vague "check logs"
// warning. Checking before spawning turns that silent stall into an
// immediate, actionable error. Mirrors daemon.resolveAuth: the daemon only
// authenticates via the stored config token, never MULTICA_TOKEN.
func requireDaemonAuth(profile string) error {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Token == "" {
		loginHint := "multica login"
		if profile != "" {
			loginHint = fmt.Sprintf("multica login --profile %s", profile)
		}
		return fmt.Errorf("you are not logged in. Run '%s' first, then start the daemon", loginHint)
	}
	return nil
}

func runDaemonStart(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon start"); err != nil {
		return err
	}
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
		// A live daemon on our port that belongs to someone else is a
		// collision, not an "already running" — starting would fail to bind
		// and "already running" would send the user looking for a daemon of
		// theirs that does not exist.
		if err := daemonIdentityMismatch(health, profile, healthPort); err != nil {
			return err
		}
		label := "daemon"
		if profile != "" {
			label = fmt.Sprintf("daemon [%s]", profile)
		}
		pid, _ := health["pid"].(float64)
		return fmt.Errorf("%s is already running (pid %v). Use 'daemon restart' to restart it", label, int(pid))
	}

	if err := requireDaemonAuth(profile); err != nil {
		return err
	}

	// Resolve current executable so the foreground child reuses this binary.
	exePath, err := daemonExecutable()
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

	// Remember where both logs end now so an early-death report below can show
	// only what THIS startup attempt wrote, not stale history. The err-log
	// offset is taken after openBoundedErrLog, which may have rolled the file.
	var logOffset int64
	if info, statErr := os.Stat(logPath); statErr == nil {
		logOffset = info.Size()
	}
	var errLogOffset int64
	if info, statErr := os.Stat(errLogPath); statErr == nil {
		errLogOffset = info.Size()
	}

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

	// Watch for early death instead of Release()ing right away: a child that
	// fails preflight (unreachable server, token rejected with 401, ...) exits
	// within seconds, and without this signal the readiness poll below would
	// blindly run out its full window before printing a vague warning. The
	// goroutine leaks Wait() for the (short) remainder of this process when
	// the daemon starts fine — the child is never killed by it and keeps
	// running independently, same as with Release().
	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()

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
		select {
		case waitErr := <-waitCh:
			return daemonStartupFailureError(daemonStartupLogs{
				logPath:      logPath,
				logOffset:    logOffset,
				errLogPath:   errLogPath,
				errLogOffset: errLogOffset,
			}, waitErr, profile, resolveDaemonServerURL(cmd, profile))
		case <-time.After(500 * time.Millisecond):
		}
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

// resolveDaemonServerURL resolves the server URL the daemon will talk to,
// in the same precedence order the foreground daemon uses: --server-url flag,
// MULTICA_SERVER_URL env, then the stored CLI config for the profile.
func resolveDaemonServerURL(cmd *cobra.Command, profile string) string {
	serverURL := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", "")
	if serverURL == "" {
		if c, err := cli.LoadCLIConfigForProfile(profile); err == nil && c.ServerURL != "" {
			serverURL = c.ServerURL
		}
	}
	return serverURL
}

// daemonStartupLogs names the two sinks a failed startup attempt may have
// written to: the child's structured slog output lands in the rotating
// daemon.log, while raw stderr — Go panics and the final cobra "Error: ..."
// line — lands in the crash sink (daemon.err.log). Both carry an offset
// recorded before the spawn so the report covers only this attempt.
type daemonStartupLogs struct {
	logPath      string
	logOffset    int64
	errLogPath   string
	errLogOffset int64
}

// daemonStartupFailureError turns a daemon child that exited before ever
// reporting ready into a friendly, actionable error. The two failure modes
// users actually hit — stored token rejected by the server, server not
// reachable — are recognized from what this startup attempt appended to the
// structured log and the crash sink, and rendered as one reason line plus a
// next step. Anything else falls back to a log excerpt with DBG/INF noise
// dropped.
func daemonStartupFailureError(logs daemonStartupLogs, waitErr error, profile, serverURL string) error {
	lines := readLogTailSince(logs.logPath, logs.logOffset, 40)
	crashLines := readLogTailSince(logs.errLogPath, logs.errLogOffset, 40)
	joined := strings.Join(append(append([]string{}, lines...), crashLines...), "\n")

	loginHint := "multica login"
	if profile != "" {
		loginHint += " --profile " + profile
	}

	switch {
	case strings.Contains(joined, "auth token rejected") ||
		strings.Contains(joined, "returned 401") ||
		strings.Contains(joined, "not authenticated"):
		return fmt.Errorf("daemon failed to start: the server rejected your login token (it may have expired or been revoked).\nRun '%s' to sign in again, then rerun 'multica daemon start'.\nFull log: %s", loginHint, logs.logPath)
	case strings.Contains(joined, "connection refused") ||
		strings.Contains(joined, "no such host") ||
		strings.Contains(joined, "i/o timeout") ||
		strings.Contains(joined, "network is unreachable"):
		target := "the Multica server"
		if serverURL != "" {
			target += " at " + serverURL
		}
		return fmt.Errorf("daemon failed to start: cannot reach %s.\nMake sure the server is running and reachable, then rerun 'multica daemon start'.\nFull log: %s", target, logs.logPath)
	}

	var b strings.Builder
	if exitErr := (&exec.ExitError{}); errors.As(waitErr, &exitErr) {
		fmt.Fprintf(&b, "daemon exited during startup (%s).", exitErr)
	} else {
		b.WriteString("daemon exited during startup.")
	}
	excerpt := filterDaemonLogNoise(lines, 5)
	// Crash output is already raw (panics, the final cobra error line) — no
	// noise filter, just cap it like the structured excerpt.
	if len(crashLines) > 5 {
		crashLines = crashLines[len(crashLines)-5:]
	}
	excerpt = append(excerpt, crashLines...)
	if len(excerpt) > 0 {
		b.WriteString("\nLog output:")
		for _, line := range excerpt {
			fmt.Fprintf(&b, "\n  %s", line)
		}
	}
	fmt.Fprintf(&b, "\nFull log: %s", logs.logPath)
	if len(crashLines) > 0 {
		fmt.Fprintf(&b, "\nCrash output: %s", logs.errLogPath)
	}
	return errors.New(b.String())
}

// filterDaemonLogNoise drops DBG/INF log lines from an excerpt, keeping
// WRN/ERR entries and plain (non-slog) lines such as the final error the
// child printed before exiting, capped to the last max lines.
func filterDaemonLogNoise(lines []string, max int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, " DBG ") || strings.Contains(line, " INF ") {
			continue
		}
		out = append(out, line)
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// readLogTailSince returns up to maxLines of the log content appended after
// sinceOffset. Reads are capped at 64 KiB so a huge burst can't balloon the
// error report; on any read problem it returns nil and the caller just points
// at the log file instead.
func readLogTailSince(logPath string, sinceOffset int64, maxLines int) []string {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	const readCap = 64 * 1024
	if info, err := f.Stat(); err == nil && info.Size()-sinceOffset > readCap {
		sinceOffset = info.Size() - readCap
	}
	if _, err := f.Seek(sinceOffset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, readCap))
	if err != nil || len(data) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]string, 0, maxLines)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
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
	if v := flagString(cmd, "workspaces-root"); v != "" {
		args = append(args, "--workspaces-root", v)
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
	if b, _ := cmd.Flags().GetBool("no-auto-reload"); b {
		args = append(args, "--no-auto-reload")
	}
	if b, _ := cmd.Flags().GetBool("no-platform-context"); b {
		args = append(args, "--no-platform-context")
	} else if v := flagString(cmd, "platform-context"); v != "" {
		args = append(args, "--platform-context", v)
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

	// Load the profile config once — several daemon knobs fall back to
	// values persisted here when both the CLI flag and the env var are
	// unset. Errors reading the config are non-fatal for anything other
	// than server URL: an unreadable / missing config only means "no
	// persisted overrides", not "cannot start".
	fileCfg, _ := cli.LoadCLIConfigForProfile(profile)
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

	serverURL := resolveDaemonServerURL(cmd, profile)
	// Each persistable daemon knob follows the same three-tier precedence:
	//
	//   --flag  >  MULTICA_… env  >  config.json  >  built-in default
	//
	// The flag/env pair is resolved inside daemon.LoadConfig (via
	// Overrides + envOrDefault). Here we only substitute the config-file
	// value when BOTH the flag AND the env are unset, so a live env
	// override (typical for systemd units) always wins over the file. This
	// matches server_url's precedence above and resolves #3824.
	deviceNameFlag := resolveDaemonStringOverride(
		flagString(cmd, "device-name"),
		"MULTICA_DAEMON_DEVICE_NAME",
		fileCfg.DeviceName,
	)
	runtimeNameFlag := resolveDaemonStringOverride(
		flagString(cmd, "runtime-name"),
		"MULTICA_AGENT_RUNTIME_NAME",
		fileCfg.RuntimeName,
	)
	workspacesRoot, err := resolveWorkspacesRootForProfile(profile, flagString(cmd, "workspaces-root"))
	if err != nil {
		return err
	}

	overrides := daemon.Overrides{
		ServerURL:      serverURL,
		DaemonID:       flagString(cmd, "daemon-id"),
		DeviceName:     deviceNameFlag,
		RuntimeName:    runtimeNameFlag,
		WorkspacesRoot: workspacesRoot,
		Profile:        profile,
		HealthPort:     healthPortForProfile(profile),
	}
	pollFlag, _ := cmd.Flags().GetDuration("poll-interval")
	pollOverride, err := resolveDaemonDurationOverride(pollFlag, "MULTICA_DAEMON_POLL_INTERVAL", fileCfg.PollInterval)
	if err != nil {
		return err
	}
	if pollOverride > 0 {
		overrides.PollInterval = pollOverride
	}
	heartbeatFlag, _ := cmd.Flags().GetDuration("heartbeat-interval")
	heartbeatOverride, err := resolveDaemonDurationOverride(heartbeatFlag, "MULTICA_DAEMON_HEARTBEAT_INTERVAL", fileCfg.HeartbeatInterval)
	if err != nil {
		return err
	}
	if heartbeatOverride > 0 {
		overrides.HeartbeatInterval = heartbeatOverride
	}
	// Distinguish "flag not passed" from an explicit `--agent-timeout 0` so a
	// user can turn off an env-configured cap from the CLI. The persisted
	// config.json value uses the same tri-state (see cli.CLIConfig.AgentTimeout)
	// so `config set agent_timeout 0s` can survive daemon restarts.
	agentTimeoutOverride, err := resolveDaemonAgentTimeoutOverride(cmd, "MULTICA_AGENT_TIMEOUT", fileCfg.AgentTimeout)
	if err != nil {
		return err
	}
	if agentTimeoutOverride != nil {
		overrides.AgentTimeout = agentTimeoutOverride
	}
	semanticFlag, _ := cmd.Flags().GetDuration("codex-semantic-inactivity-timeout")
	semanticOverride, err := resolveDaemonDurationOverride(semanticFlag, "MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT", fileCfg.CodexSemanticInactivityTimeout)
	if err != nil {
		return err
	}
	if semanticOverride > 0 {
		overrides.CodexSemanticInactivityTimeout = semanticOverride
	}
	handshakeFlag, _ := cmd.Flags().GetDuration("codex-handshake-timeout")
	handshakeOverride, err := resolveDaemonDurationOverride(handshakeFlag, "MULTICA_CODEX_HANDSHAKE_TIMEOUT", fileCfg.CodexHandshakeTimeout)
	if err != nil {
		return err
	}
	if handshakeOverride > 0 {
		overrides.CodexHandshakeTimeout = handshakeOverride
	}
	maxFlag, _ := cmd.Flags().GetInt("max-concurrent-tasks")
	if n := resolveDaemonIntOverride(maxFlag, "MULTICA_DAEMON_MAX_CONCURRENT_TASKS", fileCfg.MaxConcurrentTasks); n > 0 {
		overrides.MaxConcurrentTasks = n
	}
	// --no-auto-update / MULTICA_DAEMON_AUTO_UPDATE=false / config.json
	// disable_auto_update are all single-direction: none of them can force
	// auto-update *on*, they can only turn it off. Env "true" is not
	// treated as an override signal here — LoadConfig honors the raw env
	// itself for the affirmative case.
	noAutoUpdateFlag, _ := cmd.Flags().GetBool("no-auto-update")
	if resolveDaemonDisableSignal(noAutoUpdateFlag, "MULTICA_DAEMON_AUTO_UPDATE", fileCfg.DisableAutoUpdate) {
		overrides.DisableAutoUpdate = true
	}
	// Same single-direction shape for the on-disk version watcher, resolved
	// through its own env var: turning off GitHub polling must not also stop the
	// daemon from following a binary the operator replaced by hand.
	noAutoReloadFlag, _ := cmd.Flags().GetBool("no-auto-reload")
	if resolveDaemonDisableSignal(noAutoReloadFlag, "MULTICA_DAEMON_AUTO_RELOAD", fileCfg.DisableAutoReload) {
		overrides.DisableAutoReload = true
	}
	// Platform context is a tri-state. Explicit flags win over env, env wins
	// over the profile, and the daemon defaults to the historical full brief.
	platformContextFlag, _ := cmd.Flags().GetString("platform-context")
	noPlatformContextFlag, _ := cmd.Flags().GetBool("no-platform-context")
	if noPlatformContextFlag && strings.TrimSpace(platformContextFlag) != "" {
		return fmt.Errorf("--platform-context and --no-platform-context are mutually exclusive")
	}
	switch {
	case noPlatformContextFlag:
		overrides.DisablePlatformContext = true
	case platformContextFlag != "":
		overrides.PlatformContextMode = platformContextFlag
	case strings.TrimSpace(os.Getenv("MULTICA_PLATFORM_CONTEXT")) == "" && fileCfg.PlatformContextMode != "":
		overrides.PlatformContextMode = fileCfg.PlatformContextMode
	}
	autoUpdateFlag, _ := cmd.Flags().GetDuration("auto-update-interval")
	autoUpdateOverride, err := resolveDaemonDurationOverride(autoUpdateFlag, "MULTICA_DAEMON_AUTO_UPDATE_INTERVAL", fileCfg.AutoUpdateCheckInterval)
	if err != nil {
		return err
	}
	if autoUpdateOverride > 0 {
		overrides.AutoUpdateCheckInterval = autoUpdateOverride
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

// requireDaemonRestartPreflight validates, before a running daemon is
// stopped, that its replacement could actually start. requireDaemonAuth's
// empty-token check is not enough here: a stored token can be expired or
// revoked, and the server can be unreachable — either passes the local check,
// kills the working daemon in the stop phase, and then fails the replacement
// child's preflight, leaving no daemon at all (#5165). So probe the server
// the daemon will talk to with the stored token (same whoami call as
// `multica auth status`) and refuse to touch the running daemon on failure.
func requireDaemonRestartPreflight(cmd *cobra.Command, profile string) error {
	if err := requireDaemonAuth(profile); err != nil {
		return err
	}
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}

	rawURL := resolveDaemonServerURL(cmd, profile)
	if rawURL == "" {
		rawURL = daemon.DefaultServerURL
	}
	baseURL, err := daemon.NormalizeServerBaseURL(rawURL)
	if err != nil {
		return fmt.Errorf("refusing to restart: invalid server URL %q: %w", rawURL, err)
	}

	loginHint := "multica login"
	if profile != "" {
		loginHint += " --profile " + profile
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	if err := cli.NewAPIClient(baseURL, "", cfg.Token).GetJSON(ctx, "/api/me", nil); err != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) {
			if httpErr.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("refusing to restart: the server rejected your login token (it may have expired or been revoked); the running daemon was left untouched.\nRun '%s' to sign in again, then rerun 'multica daemon restart'", loginHint)
			}
			return fmt.Errorf("refusing to restart: preflight check against %s failed (%w); the running daemon was left untouched", baseURL, err)
		}
		return fmt.Errorf("refusing to restart: cannot reach the Multica server at %s (%w); the running daemon was left untouched.\nMake sure the server is running and reachable, then rerun 'multica daemon restart'", baseURL, err)
	}
	return nil
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	if err := requireHumanLocalCommand("daemon restart"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
	healthPort := healthPortForProfile(profile)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	if daemonAlive(health) {
		// Identity before preflight, and well before the stop phase: restart
		// kills whatever answered on this port, so a collision would take out
		// another profile's daemon and then start ours in its place.
		if err := daemonIdentityMismatch(health, profile, healthPort); err != nil {
			return err
		}
		// Validate the restart can succeed BEFORE the stop phase, not just
		// inside the start phase: a missing/revoked token or an unreachable
		// server would otherwise kill the running daemon and then fail to
		// start a replacement, leaving no daemon at all. When nothing is
		// running there is nothing to lose, so the cheaper start-path checks
		// (empty-token guard + early child-exit detection) handle it without
		// this extra server round-trip.
		if err := requireDaemonRestartPreflight(cmd, profile); err != nil {
			return err
		}
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
	if err := requireHumanLocalCommand("daemon stop"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
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
	// The PID to kill comes from whatever answered on this port, so verify it
	// is ours before signalling it.
	if err := daemonIdentityMismatch(health, profile, healthPort); err != nil {
		return err
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
	healthPort, err := daemonStatusHealthPort(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")

	// Safe to list profiles here: daemonStatusHealthPort already rejected an
	// explicit --profile inside a daemon-managed task.
	if err := requireKnownProfile(profile); err != nil {
		var unknown *unknownProfileError
		if output == "json" && errors.As(err, &unknown) {
			// Keep stdout a single valid JSON document. The non-zero exit
			// carries the failure, so errSilent suppresses the stderr copy.
			if printErr := cli.PrintJSON(os.Stdout, unknown.jsonPayload()); printErr != nil {
				return printErr
			}
			return errSilent
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := checkDaemonHealthOnPort(ctx, healthPort)

	// A daemon belonging to another profile answered on our port. Reporting it
	// as ours would be the same lie in a new place: this profile's daemon is
	// not running, and the port is simply occupied. Say exactly that, and keep
	// the top-level verdict "stopped" so scripts reading it stay correct.
	//
	// Only outside a daemon-managed task. There the port comes from the host
	// daemon's own injection rather than a profile hash, so no collision is
	// possible — and the task's profile is necessarily empty (--profile is
	// rejected) while the host may run a named one, which would make every
	// named-profile host look like a conflict and break the standing contract
	// that `daemon status` in a task reports on the daemon hosting it.
	var conflict *daemonProfileMismatchError
	if daemonAlive(health) && !inDaemonTaskIdentityContext() {
		errors.As(daemonIdentityMismatch(health, profile, healthPort), &conflict)
	}

	if output == "json" {
		if conflict != nil {
			return cli.PrintJSON(os.Stdout, map[string]any{
				"status":        "stopped",
				"port_conflict": conflict.statusJSON(),
			})
		}
		return cli.PrintJSON(os.Stdout, health)
	}

	label := "Daemon"
	if profile != "" {
		label = fmt.Sprintf("Daemon [%s]", profile)
	}

	if conflict != nil {
		fmt.Fprintf(os.Stdout, "%s: stopped\n", label)
		fmt.Fprintln(os.Stdout, conflict.statusNote())
		return nil
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

// daemonStatusHealthPort resolves which daemon `status` should probe. Outside a
// task that is the --profile-derived port, even when a stale
// MULTICA_DAEMON_PORT remains in the host environment. Inside a managed task it
// is the daemon-injected port and nothing else: healthPortForProfile hashes
// whatever profile name the caller passes, so deriving the port there would let
// a task report on a daemon that is not the one hosting it — and for a task
// hosted by a named-profile daemon it would silently probe the default daemon
// instead. A missing port fails closed rather than guessing.
func daemonStatusHealthPort(cmd *cobra.Command) (int, error) {
	profile := resolveProfile(cmd)
	if !inDaemonTaskIdentityContext() {
		return healthPortForProfile(profile), nil
	}
	if profile != "" {
		return 0, fmt.Errorf("daemon status --profile is not available inside a daemon-managed task")
	}
	raw := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_PORT"))
	if raw == "" {
		return 0, fmt.Errorf("daemon status inside a daemon-managed task requires the daemon-injected MULTICA_DAEMON_PORT")
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("invalid MULTICA_DAEMON_PORT %q inside a daemon-managed task", raw)
	}
	return port, nil
}

// describeDaemonManager turns the daemon's launched_by tag into something a
// user can act on. Unknown values are passed through rather than dropped: a
// newer daemon naming a manager this CLI predates is still worth showing.
func describeDaemonManager(launchedBy string) string {
	if launchedBy == "desktop" {
		return "Multica Desktop app (start and stop it from the app)"
	}
	return launchedBy
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
	// Answers "why is a daemon running that I never started?" — the reporter's
	// ask in #6694. Only rendered when the daemon names a manager, so a
	// standalone daemon and an older one that cannot say both stay unchanged.
	if managed, ok := health["launched_by"].(string); ok && managed != "" {
		rows = append(rows, row{"Managed by", describeDaemonManager(managed)})
	}
	// Only present while a confirmed on-disk version change is waiting for the
	// daemon to go idle, so it reads as an explanation rather than a status line.
	if reason, ok := health["reload_pending_reason"].(string); ok && reason != "" {
		rows = append(rows, row{"Restart pending", reason})
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

// tailLog is the seam tests replace to observe `daemon logs` without spawning
// the platform tail implementation.
var tailLog = tailLogFile

// profileLabel names a profile for humans; the default profile has no name.
func profileLabel(profile string) string {
	if profile == "" {
		return "default"
	}
	return profile
}

// daemonLogSourcePath resolves the daemon.log path for a profile —
// ~/.multica/daemon.log for the default profile,
// ~/.multica/profiles/<name>/daemon.log for a named one — and guarantees it is
// absolute, because this path is shown to the user to paste into an editor or
// another shell.
//
// A relative result means daemonDirForProfile could not resolve the home
// directory and swallowed the error, leaving a bare "daemon.log". That is
// reported instead of returned: it would resolve against the caller's cwd and
// name a file with nothing to do with the daemon.
func daemonLogSourcePath(profile string) (string, error) {
	logPath := daemonLogPathForProfile(profile)
	if !filepath.IsAbs(logPath) {
		return "", fmt.Errorf("cannot resolve the state directory for profile %q", profileLabel(profile))
	}
	return logPath, nil
}

func runDaemonLogs(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon logs"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
	logPath, err := daemonLogSourcePath(profile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("no log file found at %s\nThe daemon may not have been started in background mode", logPath)
	}

	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")

	// Name the file before streaming it. Which daemon.log is live is otherwise
	// unknowable — a stale default-profile log reads fine and looks current —
	// and this command is the only thing that knows the answer. It goes to
	// stderr and is written before the tail starts so `-f` output and any
	// downstream pipe stay clean.
	fmt.Fprintf(cmd.ErrOrStderr(), "Reading %s (profile: %s)\n", logPath, profileLabel(profile))

	return tailLog(logPath, lines, follow)
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

// envUnset reports whether the given env var is empty or missing entirely.
// The three-tier precedence for daemon knobs (flag > env > config.json)
// hinges on this check: config-file substitution only fires when both the
// flag AND the env are unset. Trimming whitespace matches the behavior of
// daemon.envOrDefault, so an env exported to " " reads as "not set" for
// fallback purposes — same as the runtime.
func envUnset(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) == ""
}

// resolveDaemonStringOverride picks the value that goes into
// daemon.Overrides for a string knob (device_name, runtime_name,
// workspaces_root).
// Precedence, highest first:
//
//  1. flag (already resolved to string; empty means "not passed")
//  2. env  (if set, we leave overrides empty — daemon.LoadConfig reads env)
//  3. cfg  (persisted config.json value)
//  4. ""   (leave overrides empty — daemon.LoadConfig applies its default)
//
// Split out so the precedence is testable without spinning up cobra + the
// full daemon.LoadConfig pipeline. Returns the override value the caller
// should assign; an empty string means "let daemon.LoadConfig decide".
func resolveDaemonStringOverride(flagValue, envName, cfgValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if !envUnset(envName) {
		// Env is set — return empty so daemon.LoadConfig's envOrDefault
		// resolves it. If we returned the env value here we'd double-read
		// it and quietly diverge if the daemon's own trimming ever changes.
		return ""
	}
	return cfgValue
}

// resolveWorkspacesRootForProfile is the single human-CLI resolver for the
// daemon's task workspace root. Keeping daemon start, disk-usage, aggregate
// scans, and cross-profile hints on this path prevents diagnostics from
// drifting away from the directory the daemon actually uses.
func resolveWorkspacesRootForProfile(profile, flagValue string) (string, error) {
	fileCfg, _ := cli.LoadCLIConfigForProfile(profile)
	override := resolveDaemonStringOverride(
		flagValue,
		"MULTICA_WORKSPACES_ROOT",
		fileCfg.WorkspacesRoot,
	)
	return daemon.ResolveWorkspacesRoot(profile, override)
}

// resolveDaemonDurationOverride is the numeric counterpart for
// poll_interval. flagValue > 0 wins; otherwise, if the env var is unset
// we parse cfgValue. Parse errors and non-positive values are surfaced
// so a bad config.json value doesn't silently fall through to the
// default — `config set` already rejects the same shapes at write
// time, and this guard catches legacy configs that predate that check.
func resolveDaemonDurationOverride(flagValue time.Duration, envName, cfgValue string) (time.Duration, error) {
	if flagValue > 0 {
		return flagValue, nil
	}
	if !envUnset(envName) || cfgValue == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(cfgValue)
	if err != nil {
		return 0, fmt.Errorf("config value %q for %s is not a valid duration: %w", cfgValue, envName, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("config value %q for %s must be positive", cfgValue, envName)
	}
	return parsed, nil
}

// resolveDaemonIntOverride is the max_concurrent_tasks counterpart to
// resolveDaemonStringOverride. flagValue > 0 wins; otherwise the config
// value applies only when the env var is unset.
func resolveDaemonIntOverride(flagValue int, envName string, cfgValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	if !envUnset(envName) {
		return 0
	}
	if cfgValue > 0 {
		return cfgValue
	}
	return 0
}

// resolveDaemonAgentTimeoutOverride resolves --agent-timeout across the
// same three-tier precedence used by the other daemon knobs, but preserves
// the tri-state agent_timeout semantics: nil result = "not overridden",
// non-nil = "use this exact value" (which may legitimately be zero to
// disable the wall-clock cap).
//
//  1. --agent-timeout was explicitly passed (even as `--agent-timeout 0`)
//     -> use that value; daemon.LoadConfig then bypasses env/default.
//  2. MULTICA_AGENT_TIMEOUT is set -> return nil so LoadConfig reads the
//     env itself (matches the other duration helpers).
//  3. cfgValue non-nil -> parse the persisted string and use it. "0s" is
//     valid here (it's the "disabled" sentinel).
//  4. Otherwise -> nil, LoadConfig applies DefaultAgentTimeout.
func resolveDaemonAgentTimeoutOverride(cmd *cobra.Command, envName string, cfgValue *string) (*time.Duration, error) {
	if cmd.Flags().Changed("agent-timeout") {
		d, _ := cmd.Flags().GetDuration("agent-timeout")
		return &d, nil
	}
	if !envUnset(envName) {
		return nil, nil
	}
	if cfgValue == nil || *cfgValue == "" {
		return nil, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(*cfgValue))
	if err != nil {
		return nil, fmt.Errorf("config value %q for %s is not a valid duration: %w", *cfgValue, envName, err)
	}
	if parsed < 0 {
		return nil, fmt.Errorf("config value %q for %s must be >= 0", *cfgValue, envName)
	}
	return &parsed, nil
}

// resolveDaemonDisableSignal resolves a single-direction disable signal —
// auto-update (--no-auto-update) and auto-reload (--no-auto-reload) share the
// shape. Precedence, using auto-update as the example:
//
//  1. --no-auto-update flag passed -> disable.
//  2. MULTICA_DAEMON_AUTO_UPDATE explicitly set to a falsy value ->
//     disable. Env values other than false/0/no/off are handled inside
//     daemon.LoadConfig (they may enable auto-update on self-host, which
//     is not something we can express via CLI overrides today).
//  3. config.json disable_auto_update=true (only when both flag and env
//     are silent) -> disable.
//  4. Otherwise -> leave the override off; LoadConfig picks the default.
func resolveDaemonDisableSignal(flagValue bool, envName string, cfgValue bool) bool {
	if flagValue {
		return true
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no", "off":
			return true
		}
		// Env is set to something other than a falsy value — leave the
		// override off and let LoadConfig read the raw env itself.
		return false
	}
	return cfgValue
}

// --- daemon disk-usage ---

func runDaemonDiskUsage(cmd *cobra.Command, _ []string) error {
	taskContext := inDaemonManagedExecutionContext()
	profile := resolveProfile(cmd)
	rootOverride, _ := cmd.Flags().GetString("workspaces-root")
	byWorkspace, _ := cmd.Flags().GetBool("by-workspace")
	byTask, _ := cmd.Flags().GetBool("by-task")
	top, _ := cmd.Flags().GetInt("top")
	output, _ := cmd.Flags().GetString("output")
	allProfiles, _ := cmd.Flags().GetBool("all-profiles")

	if taskContext {
		if err := checkTaskDiskUsageScope(profile, rootOverride, allProfiles); err != nil {
			return err
		}
	}
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
		return runDaemonDiskUsageAggregate(cmd, byWorkspace, top, output)
	}

	workspacesRoot, err := resolveDiskUsageRoot(taskContext, profile, rootOverride)
	if err != nil {
		return fmt.Errorf("resolve workspaces root: %w", err)
	}

	report, err := daemon.ScanDiskUsage(workspacesRoot, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}
	// Resolve before --top trims the slice: the batch endpoint costs the same
	// either way, and the JSON consumer sees statuses for everything it gets.
	// Skipped in a task: the fetcher authenticates with the Owner profile's
	// stored token, which a task must not borrow, so the column stays blank.
	if !taskContext && diskUsageNeedsParentStatus(byWorkspace, output) {
		fillDiskUsageParentStatuses(cmd, profile, &report)
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
		printDiskUsageOtherRootsHint(os.Stdout, report, profile, rootOverride, taskContext)
		return nil
	}
	printDiskUsageTaskTable(os.Stdout, report)
	printDiskUsageOtherRootsHint(os.Stdout, report, profile, rootOverride, taskContext)
	return nil
}

// checkTaskDiskUsageScope keeps a managed task's disk-usage view inside the
// daemon root that hosts it. Each rejected input widens the report past that
// boundary: --all-profiles enumerates ~/.multica/profiles and discloses the
// Owner's profile names, while --workspaces-root and --profile aim the scan at
// a directory this daemon does not manage.
func checkTaskDiskUsageScope(profile, rootOverride string, allProfiles bool) error {
	switch {
	case allProfiles:
		return fmt.Errorf("daemon disk-usage --all-profiles is not available inside a daemon-managed task")
	case rootOverride != "":
		return fmt.Errorf("daemon disk-usage --workspaces-root is not available inside a daemon-managed task")
	case profile != "":
		return fmt.Errorf("daemon disk-usage --profile is not available inside a daemon-managed task")
	}
	return nil
}

// resolveDiskUsageRoot picks the directory to scan. A managed task reads the
// daemon-injected root only: ResolveWorkspacesRoot falls back to a $HOME- and
// profile-derived default, which for a task hosted by a named-profile daemon
// resolves to the default root and reports a tree the task has nothing to do
// with. Failing closed on a missing value keeps that guess out of the output.
func resolveDiskUsageRoot(taskContext bool, profile, rootOverride string) (string, error) {
	if !taskContext {
		return resolveWorkspacesRootForProfile(profile, rootOverride)
	}
	root := strings.TrimSpace(os.Getenv(daemon.TaskWorkspacesRootEnv))
	if root == "" {
		return "", fmt.Errorf("daemon-managed task requires the daemon-injected workspaces root in %s", daemon.TaskWorkspacesRootEnv)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%s must be an absolute path", daemon.TaskWorkspacesRootEnv)
	}
	return filepath.Clean(root), nil
}

// diskUsageNeedsParentStatus reports whether this invocation renders per-task
// rows at all. The --by-workspace table has no STATUS column, so resolving
// statuses for it would spend a network round trip — and, against an
// unreachable server, a full timeout — on data nothing displays, turning a
// local diagnostic into a command that hangs offline. JSON output always
// carries the task array, so it still needs them even with --by-workspace.
func diskUsageNeedsParentStatus(byWorkspace bool, output string) bool {
	return !byWorkspace || output == "json"
}

// fillDiskUsageParentStatuses populates the report's STATUS column in place.
//
// This is best-effort and never fails the command: `disk-usage` is a local
// diagnostic that has to keep working when the machine is offline or logged
// out. When statuses can't be resolved the column simply stays blank, and the
// reason goes to stderr so `--output json` stdout stays machine-readable.
func fillDiskUsageParentStatuses(cmd *cobra.Command, profile string, report *daemon.DiskUsageReport) {
	fetch := newParentStatusFetcher(cmd, profile)
	if fetch == nil {
		return
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	if err := daemon.ResolveParentStatuses(ctx, report, fetch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve issue statuses, STATUS column may be blank: %v\n", err)
	}
}

// newParentStatusFetcher builds a fetcher backed by the same batch gc-check
// endpoint the daemon's GC loop uses, so the STATUS column reports exactly the
// status the GC would act on. The daemon authenticates with the profile's CLI
// token (see Daemon.resolveAuth), so reusing it here needs no extra API
// surface. Returns nil when this profile has no usable credentials — the
// caller then leaves the column blank rather than erroring.
func newParentStatusFetcher(cmd *cobra.Command, profile string) daemon.ParentStatusFetcher {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil || strings.TrimSpace(cfg.Token) == "" {
		return nil
	}
	rawURL := resolveDaemonServerURL(cmd, profile)
	if rawURL == "" {
		rawURL = daemon.DefaultServerURL
	}
	baseURL, err := daemon.NormalizeServerBaseURL(rawURL)
	if err != nil {
		return nil
	}

	client := daemon.NewClient(baseURL)
	client.SetToken(cfg.Token)
	return func(ctx context.Context, workspaceID string, issueIDs []string) (map[string]string, error) {
		results, err := client.GetIssueGCChecks(ctx, workspaceID, issueIDs)
		if err != nil {
			return nil, err
		}
		statuses := make(map[string]string, len(results))
		for id, result := range results {
			// Skip per-issue failures and 404s: an unresolved id must stay
			// blank so it reads as "unknown", not as a status.
			if result.Err != nil || !result.Found {
				continue
			}
			statuses[id] = result.Status
		}
		return statuses, nil
	}
}

// runDaemonDiskUsageAggregate scans every workspace root (the default root plus
// each ~/.multica/profiles/* root) and renders a per-root breakdown with a
// combined grand total. This is the path that surfaces the Desktop app's
// `desktop-<host>` root, which the default single-root scan never sees.
func runDaemonDiskUsageAggregate(cmd *cobra.Command, byWorkspace bool, top int, output string) error {
	roots, err := enumerateDiskUsageRoots()
	if err != nil {
		return err
	}
	agg, err := daemon.ScanDiskUsageRoots(roots, daemon.ArtifactPatternsFromEnv())
	if err != nil {
		return err
	}
	// Each root belongs to its own profile, so it needs that profile's token.
	// Skipping the whole loop when nothing renders task rows matters most
	// here: an unreachable server would otherwise burn one full timeout per
	// root, serially.
	if diskUsageNeedsParentStatus(byWorkspace, output) {
		for i := range agg.Roots {
			fillDiskUsageParentStatuses(cmd, agg.Roots[i].Profile, &agg.Roots[i].Report)
		}
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
	out := make([]daemon.DiskUsageRoot, 0)

	if root, err := resolveWorkspacesRootForProfile("", ""); err == nil {
		out = append(out, daemon.DiskUsageRoot{Profile: "", Root: root})
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
		root, err := resolveWorkspacesRootForProfile(name, "")
		if err != nil || containsDiskUsageRoot(out, root) {
			continue
		}
		// Skip profile roots that were never created on disk — a configured
		// profile whose daemon never ran has nothing to report.
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, daemon.DiskUsageRoot{Profile: name, Root: root})
	}
	return out, nil
}

func containsDiskUsageRoot(roots []daemon.DiskUsageRoot, candidate string) bool {
	for _, root := range roots {
		if samePath(root.Root, candidate) {
			return true
		}
	}
	return false
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
	if agg.TotalRepoCacheCount > 0 || agg.TotalRepoCacheSizeBytes > 0 {
		fmt.Fprintf(w, "Repo cache (.repos): %s across %d repo(s) in all roots, not included above.\n",
			formatBytes(agg.TotalRepoCacheSizeBytes), agg.TotalRepoCacheCount)
	}
}

func printDiskUsageTaskTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalTaskCount == 0 {
		fmt.Fprintln(w, "(no task directories)")
		printRepoCacheLine(w, report)
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
	} else {
		fmt.Fprintf(w, "\nTotal: %s across %d task(s); %s reclaimable as artifacts (%.1f%%).\n",
			formatBytes(report.TotalSizeBytes), report.TotalTaskCount,
			formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
	}
	printRepoCacheLine(w, report)
}

// printRepoCacheLine reports the bare-repo cache on its own line. Every task
// directory in a workspace checks out from this shared cache, so folding it
// into the task totals would attribute it to directories that do not contain
// it. Omitting it entirely — the previous behaviour — is what made the
// reported total silently disagree with the user's file manager.
func printRepoCacheLine(w io.Writer, report daemon.DiskUsageReport) {
	if report.RepoCacheCount == 0 && report.RepoCacheSizeBytes == 0 {
		return
	}
	fmt.Fprintf(w, "Repo cache (.repos): %s across %d repo(s), not included above. Evicted once a repo is unused for MULTICA_GC_REPO_TTL and no longer attached to a workspace.\n",
		formatBytes(report.RepoCacheSizeBytes), report.RepoCacheCount)
}

func printDiskUsageWorkspaceTable(w io.Writer, report daemon.DiskUsageReport) {
	fmt.Fprintf(w, "Workspaces root: %s\n", report.WorkspacesRoot)
	if report.TotalWorkspaceCount == 0 {
		fmt.Fprintln(w, "(no workspaces)")
		printRepoCacheLine(w, report)
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
	} else {
		fmt.Fprintf(w, "\nTotal: %s across %d workspace(s); %s reclaimable as artifacts (%.1f%%).\n",
			formatBytes(report.TotalSizeBytes), report.TotalWorkspaceCount,
			formatBytes(report.TotalArtifactSizeBytes), report.TotalArtifactRatio*100)
	}
	printRepoCacheLine(w, report)
}

// printDiskUsageOtherRootsHint warns that workspace roots OTHER than the one
// just scanned also hold task directories — the case that hides the Desktop
// app's `desktop-<host>` root behind a non-empty default root. It fires
// whenever such roots exist (empty current root or not); the only opt-out is an
// explicit --workspaces-root, where the user already chose exactly what to scan.
func printDiskUsageOtherRootsHint(w io.Writer, report daemon.DiskUsageReport, profile, rootOverride string, taskContext bool) {
	if rootOverride != "" {
		return
	}
	// The suggestions are built by listing ~/.multica/profiles, so printing
	// them inside a task would disclose the Owner's profile names — exactly
	// what rejecting --all-profiles prevents — and every command they suggest
	// is rejected there anyway.
	if taskContext {
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
		if root, err := resolveWorkspacesRootForProfile("", ""); err == nil && !samePath(root, currentRoot) {
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
		root, err := resolveWorkspacesRootForProfile(profile, "")
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
