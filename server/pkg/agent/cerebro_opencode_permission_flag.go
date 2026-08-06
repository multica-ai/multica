package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// OpenCode renamed its non-interactive permission flag, and both names are
// live in our fleet at the same time:
//
//	opencode 1.14.31 → --dangerously-skip-permissions   (rejects --auto, exit 1 + usage dump)
//	opencode 1.18.8  → --auto                           (silently ignores the old name)
//
// Hardcoding either one breaks the other half of the fleet, and the two
// failure modes look nothing alike: the older CLI is argv-strict, so the run
// dies instantly with a usage dump; the newer one accepts the unknown flag and
// then stalls on the first permission prompt because nothing auto-approved it.
// That asymmetry is why this has been "fixed" in both directions already
// (FIR-3945): whoever was on the other version saw a regression each time.
//
// So we ask the binary instead of asserting a version. `opencode run --help`
// prints the flag table for the run subcommand and exits 0.
const (
	opencodePermissionFlagCurrent = "--auto"
	opencodePermissionFlagLegacy  = "--dangerously-skip-permissions"
)

// opencodePermissionFlagCache memoises the probe per resolved executable path.
// The daemon spawns opencode once per task; re-probing every run would add a
// subprocess (~1s on a cold binary) to every single task for an answer that
// only changes when the binary is upgraded — and an upgrade replaces the
// process' view of it via a fresh daemon start anyway.
var opencodePermissionFlagCache sync.Map // execPath → string

// opencodePermissionFlag returns the non-interactive permission flag that the
// opencode binary at execPath actually advertises. Falls back to the current
// upstream name when the probe cannot be completed — emitting no flag at all
// is strictly worse, because the run would then block on the first permission
// prompt with no one to answer it.
func opencodePermissionFlag(execPath string, logger *slog.Logger) string {
	if cached, ok := opencodePermissionFlagCache.Load(execPath); ok {
		return cached.(string)
	}
	flag := selectOpencodePermissionFlag(probeOpencodeRunHelp(execPath), execPath, logger)
	opencodePermissionFlagCache.Store(execPath, flag)
	return flag
}

// probeOpencodeRunHelp returns `opencode run --pure --help` output, or "" if
// the binary could not be asked. `--pure` skips external plugins so a broken
// plugin in the user's config cannot hang or poison the probe.
func probeOpencodeRunHelp(execPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, execPath, "run", "--pure", "--help")
	cmd.Env = append(os.Environ(),
		"OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=true",
		"OPENCODE_DISABLE_MODELS_FETCH=true",
	)
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// selectOpencodePermissionFlag picks the flag advertised by the given help
// text. Split out from the probe so it is testable without a real binary.
func selectOpencodePermissionFlag(help, execPath string, logger *slog.Logger) string {
	switch {
	case strings.Contains(help, opencodePermissionFlagCurrent):
		return opencodePermissionFlagCurrent
	case strings.Contains(help, opencodePermissionFlagLegacy):
		return opencodePermissionFlagLegacy
	}
	if logger != nil {
		logger.Warn("opencode advertises neither permission flag; falling back",
			"exec", execPath,
			"fallback", opencodePermissionFlagCurrent,
			"probed_help_bytes", len(help),
		)
	}
	return opencodePermissionFlagCurrent
}
