package main

// Boot autostart for the local agent runtime daemon.
//
// `multica daemon start` registers the profile's daemon with the OS so the
// machine brings it back after a reboot / re-login — without that, every
// reboot silently takes the runtime offline and queued runs sit unclaimed
// until someone notices. Each platform registers a start-at-login entry that
// re-invokes `multica daemon start --foreground` for the same profile:
//
//	Windows   HKCU\...\Run value           (per-user, no elevation)
//	macOS     ~/Library/LaunchAgents plist (per-user, runs at login)
//	Linux     systemd user unit, or an XDG autostart .desktop fallback
//	          when no systemd user session is available
//
// Registration is a side effect of `daemon start` (skip with
// --no-autostart) and is managed explicitly through
// `multica daemon autostart enable|disable|status`. Daemons spawned by a
// manager — the Desktop app, which has its own start-at-login preference —
// are never registered: the manager owns that daemon's lifecycle.
//
// The registered command runs the FOREGROUND daemon on purpose: launchd,
// systemd, and a login session all supervise one long-lived process, whereas
// the background launcher would spawn a child and sit polling for up to 45s.

import (
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Mechanism keys reported by status (`--output json`). Each maps to a human
// label through mechanismLabel for table output.
const (
	autostartMechanismWindowsRun  = "windows-run-key"
	autostartMechanismLaunchd     = "launchd"
	autostartMechanismSystemd     = "systemd"
	autostartMechanismXDG         = "xdg-autostart"
	autostartMechanismUnsupported = "unsupported"
)

// autostartState is the platform-agnostic view of one profile's boot
// autostart entry: whether it is registered, under which mechanism, where the
// registration lives, and what command it launches.
//
// Location is populated even while disabled — it names the file / registry
// value `enable` would write, so `status` answers "where would this go?"
// before the user commits to anything.
type autostartState struct {
	Enabled   bool   `json:"enabled"`
	Mechanism string `json:"mechanism"`
	Location  string `json:"location,omitempty"`
	Command   string `json:"command,omitempty"`
	// Note carries an optional platform follow-up the user should act on
	// (currently: systemd machines that cannot start the user manager at
	// boot without linger). Kept in the state rather than printed inside
	// the platform writer so both `enable` and `daemon start` render it.
	Note string `json:"note,omitempty"`
}

// autostartSpec is the login command to register: the resolved executable,
// its argv, and the PATH captured from the registering shell.
//
// PathEnv matters on macOS and Linux, where launchd / the systemd user
// manager start daemons with a minimal PATH that would miss agent CLIs
// installed through Homebrew, nvm, or a user-level bin directory. Windows
// needs no capture: a Run-key entry inherits the user's registry environment,
// which is what a fresh login would get anyway. The snapshot refreshes on
// every `daemon start`, so it tracks the shell the user actually runs from.
type autostartSpec struct {
	Exe     string
	Args    []string
	PathEnv string
}

// Platform seams. Each build defines the four platform* functions; they are
// variables so tests can exercise the enable/disable/status flows without
// touching the developer's real registry, LaunchAgents, or systemd user
// directory.
var (
	autostartSupported = platformAutostartSupported
	writeAutostart     = platformWriteAutostart
	removeAutostart    = platformRemoveAutostart
	readAutostart      = platformReadAutostart
)

// ensureDaemonAutostart is the `daemon start` hook, behind a seam for the
// same reason: the lifecycle paths under test must not write autostart state
// onto the machine running the tests.
var ensureDaemonAutostart = ensureDaemonAutostartDefault

// ---------------------------------------------------------------------------
// command wiring
// ---------------------------------------------------------------------------

var daemonAutostartCmd = &cobra.Command{
	Use:   "autostart",
	Short: "Manage boot autostart for this profile's daemon",
	Long: "Manage whether this profile's daemon starts automatically at login/boot.\n\n" +
		"'multica daemon start' registers autostart by default (skip with --no-autostart). " +
		"The OS entry re-runs 'multica daemon start --foreground' for this profile: a Run key " +
		"on Windows, a launchd LaunchAgent on macOS, a systemd user unit — or an XDG autostart " +
		"entry when systemd is unavailable — on Linux. 'daemon stop' stops the daemon for now " +
		"and does not remove the registration.",
}

var daemonAutostartEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Register the daemon to start automatically at login/boot",
	RunE:  runDaemonAutostartEnable,
}

var daemonAutostartDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the daemon's login/boot autostart registration",
	RunE:  runDaemonAutostartDisable,
}

var daemonAutostartStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether this profile's daemon starts automatically",
	RunE:  runDaemonAutostartStatus,
}

func init() {
	daemonCmd.AddCommand(daemonAutostartCmd)
	daemonAutostartCmd.AddCommand(daemonAutostartEnableCmd)
	daemonAutostartCmd.AddCommand(daemonAutostartDisableCmd)
	daemonAutostartCmd.AddCommand(daemonAutostartStatusCmd)
	daemonAutostartStatusCmd.Flags().String("output", "table", "Output format: table or json")
}

// requireAutostartSupported rejects the platform action on a GOOS with no
// registration mechanism. `daemon start` treats the same condition as "stay
// silent and skip" — a start must not fail on an exotic platform over an
// optional convenience — but an explicit autostart command has nothing else
// to do and says so instead of pretending it worked.
func requireAutostartSupported() error {
	if autostartSupported() {
		return nil
	}
	return fmt.Errorf("boot autostart is not supported on %s", runtime.GOOS)
}

func runDaemonAutostartEnable(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon autostart enable"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
	if err := requireAutostartSupported(); err != nil {
		return err
	}
	spec, err := autostartSpecFor(profile)
	if err != nil {
		return err
	}
	state, changed, err := writeAutostart(profile, spec)
	if err != nil {
		return err
	}

	verb := "already enabled"
	if changed {
		verb = "enabled"
	}
	fmt.Fprintf(os.Stderr, "Boot autostart %s for profile %s (%s) — the daemon starts at login.\n",
		verb, profileLabel(profile), mechanismLabel(state.Mechanism))
	if state.Location != "" {
		fmt.Fprintf(os.Stderr, "Location: %s\n", state.Location)
	}
	if state.Note != "" {
		fmt.Fprintf(os.Stderr, "Note: %s\n", state.Note)
	}
	return nil
}

func runDaemonAutostartDisable(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon autostart disable"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	// Deliberately no requireKnownProfile here: removal has to keep working
	// after the profile's state directory is gone — cleaning up a stale
	// registration is exactly when the profile may no longer exist. A typo
	// still reports honestly ("not enabled") instead of silently no-opping
	// behind a validation error.
	if err := requireAutostartSupported(); err != nil {
		return err
	}
	_, changed, err := removeAutostart(profile)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(os.Stderr, "Boot autostart is not enabled for profile %s.\n", profileLabel(profile))
		return nil
	}
	fmt.Fprintf(os.Stderr, "Boot autostart disabled for profile %s — the daemon no longer starts at login.\n",
		profileLabel(profile))
	return nil
}

// autostartStatusReport is the `--output json` document: the profile plus the
// platform state, inlined so consumers read one flat object.
type autostartStatusReport struct {
	Profile string `json:"profile"`
	autostartState
}

func runDaemonAutostartStatus(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon autostart status"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
	state, err := readAutostart(profile)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, autostartStatusReport{Profile: profile, autostartState: state})
	}
	printAutostartReport(os.Stdout, profile, state)
	return nil
}

// printAutostartReport renders the table view as aligned key/value rows,
// matching how `daemon status` prints its summary.
func printAutostartReport(w io.Writer, profile string, state autostartState) {
	status := "disabled"
	if state.Enabled {
		status = "enabled"
	}
	rows := []struct{ key, value string }{
		{"Profile", profileLabel(profile)},
		{"Autostart", status},
		{"Mechanism", mechanismLabel(state.Mechanism)},
	}
	if state.Location != "" {
		rows = append(rows, struct{ key, value string }{"Location", state.Location})
	}
	if state.Command != "" {
		rows = append(rows, struct{ key, value string }{"Command", state.Command})
	}
	if state.Note != "" {
		rows = append(rows, struct{ key, value string }{"Note", state.Note})
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

func mechanismLabel(key string) string {
	switch key {
	case autostartMechanismWindowsRun:
		return "Windows Run key"
	case autostartMechanismLaunchd:
		return "launchd LaunchAgent"
	case autostartMechanismSystemd:
		return "systemd user unit"
	case autostartMechanismXDG:
		return "XDG autostart"
	case autostartMechanismUnsupported:
		return "unsupported on " + runtime.GOOS
	case "":
		return "unknown"
	default:
		return key
	}
}

// ---------------------------------------------------------------------------
// the `daemon start` hook
// ---------------------------------------------------------------------------

// shouldRegisterAutostart decides whether an implicit registration (from
// `daemon start`) applies to this invocation:
//
//   - --no-autostart is an explicit opt-out for this run.
//   - MULTICA_LAUNCHED_BY names a manager (the Desktop app) that spawned the
//     daemon and owns its lifecycle — including its own start-at-login
//     preference — so registering underneath it would fight that setting.
//   - unsupported platforms skip silently; see requireAutostartSupported.
func shouldRegisterAutostart(cmd *cobra.Command) bool {
	if !autostartSupported() {
		return false
	}
	if v, _ := cmd.Flags().GetBool("no-autostart"); v {
		return false
	}
	return os.Getenv("MULTICA_LAUNCHED_BY") == ""
}

// ensureDaemonAutostartDefault refreshes the profile's registration so every
// successful `daemon start` leaves the machine configured to bring the daemon
// back at login. Rewriting is idempotent: a registration that already matches
// is left untouched, which is also what heals a stale executable path after
// the binary moved (a Homebrew upgrade, a self-update).
//
// Failures warn instead of failing the start — the daemon itself is the
// deliverable of `daemon start`; autostart is the persistence layer on top.
func ensureDaemonAutostartDefault(cmd *cobra.Command, profile string, announce bool) {
	if !shouldRegisterAutostart(cmd) {
		return
	}
	spec, err := autostartSpecFor(profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not register boot autostart: %v\n", err)
		return
	}
	state, _, err := writeAutostart(profile, spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not register boot autostart: %v\n", err)
		return
	}
	if !announce {
		return
	}
	fmt.Fprintf(os.Stderr, "Boot autostart: enabled (%s) — this profile's daemon starts at login. Manage: multica daemon autostart status\n",
		mechanismLabel(state.Mechanism))
	if state.Note != "" {
		fmt.Fprintf(os.Stderr, "Note: %s\n", state.Note)
	}
}

// autostartSpecFor resolves the exact command a login entry should run.
func autostartSpecFor(profile string) (autostartSpec, error) {
	exe, err := daemonExecutable()
	if err != nil {
		return autostartSpec{}, fmt.Errorf("resolve executable path: %w", err)
	}
	return autostartSpec{
		Exe:     exe,
		Args:    autostartArgs(profile),
		PathEnv: os.Getenv("PATH"),
	}, nil
}

// autostartArgs is the argv registered for a profile: the foreground daemon
// (the process login entries supervise) with the profile flag when named.
// Everything else — server URL, workspaces root, tunables — comes from the
// profile's config.json at runtime, so the entry never goes stale when a
// setting changes.
func autostartArgs(profile string) []string {
	args := []string{"daemon", "start", "--foreground"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return args
}

// autostartProfileSlug renders a profile name as a filename- and
// unit-name-safe token. Characters outside [A-Za-z0-9._-] become '-', and
// when anything changed a short hash of the original is appended so two
// distinct profiles that sanitize to the same text (team/dev vs team-dev)
// still get distinct entries.
func autostartProfileSlug(profile string) string {
	var b strings.Builder
	changed := false
	for _, r := range profile {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
			changed = true
		}
	}
	slug := b.String()
	if !changed {
		return slug
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(profile))
	return fmt.Sprintf("%s-%06x", slug, h.Sum32()&0xffffff)
}

// ---------------------------------------------------------------------------
// platform-agnostic builders (pure; exercised by tests on every OS)
// ---------------------------------------------------------------------------

// windowsQuoteArg quotes one argv element for a Windows command line using
// the CRT parsing rules (backslashes before quotes and at the end of a
// quoted run are doubled). Unquoted when no quoting is needed.
func windowsQuoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat("\\", backslashes*2+1))
			b.WriteByte('"')
			backslashes = 0
		default:
			b.WriteString(strings.Repeat("\\", backslashes))
			backslashes = 0
			b.WriteByte(s[i])
		}
	}
	b.WriteString(strings.Repeat("\\", backslashes*2))
	b.WriteByte('"')
	return b.String()
}

// windowsRunValueName is the HKCU Run value this profile owns. Distinct per
// profile so several daemons on one machine never overwrite each other.
func windowsRunValueName(profile string) string {
	if profile == "" {
		return "Multica"
	}
	return "Multica (" + profile + ")"
}

// windowsAutostartCommand renders the Run value: a single command line, since
// the registry stores no argv array.
func windowsAutostartCommand(spec autostartSpec) string {
	parts := make([]string, 0, len(spec.Args)+1)
	parts = append(parts, windowsQuoteArg(spec.Exe))
	for _, a := range spec.Args {
		parts = append(parts, windowsQuoteArg(a))
	}
	return strings.Join(parts, " ")
}

// launchAgentLabel is the reverse-DNS label shared by the LaunchAgent's
// filename and its launchd Label key.
func launchAgentLabel(profile string) string {
	if profile == "" {
		return "ai.multica.daemon"
	}
	return "ai.multica.daemon." + autostartProfileSlug(profile)
}

// launchAgentPlistContent renders the LaunchAgent. RunAtLoad starts it at
// login; there is deliberately no KeepAlive — `multica daemon stop` and
// logout must stay authoritative, and launchd would otherwise fight them by
// restarting the daemon forever.
func launchAgentPlistContent(spec autostartSpec, label string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistEntry(&b, "Label", label)
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range append([]string{spec.Exe}, spec.Args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlEscape(arg))
	}
	b.WriteString("\t</array>\n")
	// The login session's PATH is the only environment launchd does not
	// provide; embed the snapshot the registering shell handed us so agent
	// CLIs outside the system directories stay discoverable.
	if spec.PathEnv != "" {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		writePlistEntry(&b, "PATH", spec.PathEnv)
		b.WriteString("\t</dict>\n")
	}
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	// Background process type: a poll/websocket daemon wants neither App Nap
	// nor full user-interactive QoS.
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func writePlistEntry(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<string>%s</string>\n", xmlEscape(key), xmlEscape(value))
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// autostartCommandDisplay renders a spec as a readable command line for
// status output. File-based mechanisms keep their argv in structured form
// (plist array, unit file) where no shell quoting applies, so a plain join is
// the honest rendering; the Windows registry stores a literal command line
// and reports that instead.
func autostartCommandDisplay(spec autostartSpec) string {
	return strings.Join(append([]string{spec.Exe}, spec.Args...), " ")
}

// launchAgentStoredCommand extracts ProgramArguments from a written plist so
// status reports the registered command as stored — including an executable
// path a later `daemon start` has not refreshed yet — rather than a freshly
// resolved guess. The plist's only <array> is ProgramArguments.
func launchAgentStoredCommand(content []byte) string {
	var doc struct {
		Dict struct {
			Arrays []struct {
				Strings []string `xml:"string"`
			} `xml:"array"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(content, &doc); err != nil || len(doc.Dict.Arrays) == 0 {
		return ""
	}
	return strings.Join(doc.Dict.Arrays[0].Strings, " ")
}

// systemdStoredCommand extracts the ExecStart line from a written unit.
func systemdStoredCommand(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			return strings.TrimPrefix(line, "ExecStart=")
		}
	}
	return ""
}

// xdgStoredCommand extracts the Exec line from a written .desktop entry.
func xdgStoredCommand(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Exec=") {
			return strings.TrimPrefix(line, "Exec=")
		}
	}
	return ""
}

// systemdUnitName is the user-unit filename for a profile (file and unit
// name are the same string in a systemd user directory).
func systemdUnitName(profile string) string {
	if profile == "" {
		return "multica-daemon.service"
	}
	return "multica-daemon-" + autostartProfileSlug(profile) + ".service"
}

// systemdExecArg escapes one ExecStart word: '%' is a systemd specifier and
// must be doubled, and anything whitespace/quote-bearing goes through
// systemd's C-style quoting.
func systemdExecArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// systemdUnitContent renders the user unit.
//
// Restart=on-failure with a start limit bounds retries: a boot that races
// the network gets a few chances to reach the server, while a persistent
// failure (a manual daemon already holding the health port, a missing login)
// stops after StartLimitBurst attempts instead of spinning forever.
// `multica daemon stop` exits 0, so the restart policy never undoes it.
func systemdUnitContent(spec autostartSpec) string {
	exec := make([]string, 0, len(spec.Args)+1)
	exec = append(exec, systemdExecArg(spec.Exe))
	for _, a := range spec.Args {
		exec = append(exec, systemdExecArg(a))
	}

	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Multica agent runtime daemon\n")
	b.WriteString("StartLimitIntervalSec=120\n")
	b.WriteString("StartLimitBurst=5\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("ExecStart=" + strings.Join(exec, " ") + "\n")
	if spec.PathEnv != "" {
		b.WriteString(`Environment="PATH=` + systemdEscapeEnvValue(spec.PathEnv) + "\"\n")
	}
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=10\n")
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

// systemdEscapeEnvValue escapes a value inside a quoted systemd setting:
// '%' is specifier-expanded there too, and backslash starts C escapes.
func systemdEscapeEnvValue(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, `\`, `\\`)
	return s
}

// xdgAutostartFileName is the .desktop filename under ~/.config/autostart.
func xdgAutostartFileName(profile string) string {
	if profile == "" {
		return "multica-daemon.desktop"
	}
	return "multica-daemon-" + autostartProfileSlug(profile) + ".desktop"
}

// desktopExecArg escapes one Exec word per the desktop-entry spec: '%'
// introduces field codes (double it), and reserved characters require
// double-quoted grouping with \ and " backslash-escaped inside.
func desktopExecArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'`$&;|<>~*?#(){}[]\\") {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// xdgAutostartContent renders the fallback entry used when systemd is not
// available. The session manager starts it at login with the session's
// environment, which — like launchd — may lack the shell-only PATH pieces,
// so PATH is injected through `env`. NoDisplay keeps it out of application
// menus; it exists only to autostart.
func xdgAutostartContent(spec autostartSpec) string {
	execWords := []string{"env"}
	if spec.PathEnv != "" {
		execWords = append(execWords, "PATH="+spec.PathEnv)
	}
	execWords = append(execWords, spec.Exe)
	execWords = append(execWords, spec.Args...)

	quoted := make([]string, 0, len(execWords))
	for _, w := range execWords {
		quoted = append(quoted, desktopExecArg(w))
	}

	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=1.0\n")
	b.WriteString("Name=Multica daemon\n")
	b.WriteString("Comment=Start the Multica agent runtime daemon at login\n")
	b.WriteString("Exec=" + strings.Join(quoted, " ") + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("NoDisplay=true\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	return b.String()
}
