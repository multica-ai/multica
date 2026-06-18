package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

// managedZdotdir returns the path to a daemon-managed ZDOTDIR whose startup
// files source the user's real zsh rc files and then re-prepend binDir to PATH.
// It returns "" (and injects nothing) when the mechanism is unsupported.
//
// Why this exists
//
// The daemon prepends binDir (the directory of the running multica binary) to
// the spawned agent's PATH (see daemon.go, agentEnv["PATH"]). That makes the
// agent's *main* process resolve `multica` to the daemon's own binary. But
// coding agents (Claude Code, OpenClaw, etc.) run their shell tool-calls
// through a LOGIN shell — `zsh -lc` / `zsh -ilc`. A login zsh re-sources the
// user's ~/.zprofile, which on a typical macOS dev box runs
// `eval "$(/opt/homebrew/bin/brew shellenv)"` and re-prepends /opt/homebrew/bin
// to PATH. If the user also has a Homebrew-installed `multica`, that stale
// binary now shadows the daemon's binDir, and `multica channel ...` (or any
// command newer than the brew build) fails with "unknown command". See the
// OPE-1943 investigation: the agent tried `multica channel context` and the
// brew binary rejected it.
//
// PATH ordering in the inherited env cannot win this race: the login shell
// rebuilds PATH from the rc files every time, after our injection. The only
// hook that survives is ZDOTDIR — point zsh at a directory we control, source
// the user's real rc files from it (preserving fnm/nvm/go/etc.), then prepend
// binDir last so it beats brew shellenv.
//
// Scope / known limits
//
//   - zsh only. bash login shells have no ZDOTDIR equivalent and BASH_ENV does
//     not run for login shells, so the only bash hook would be writing to the
//     user's ~/.bash_profile — unacceptable. macOS defaults to zsh since
//     Catalina and the affected population (brew-installed multica + dev
//     daemon) is overwhelmingly zsh. bash users keep the old behaviour.
//   - Windows has no zsh; skipped.
//
// The managed directory is daemon-global (content depends only on binDir and
// $HOME), created once and reused for every task. There is nothing per-task to
// clean up.
func (d *Daemon) managedZdotdir() string {
	d.zdotdirOnce.Do(func() {
		d.zdotdirPath = d.setupManagedZdotdir()
	})
	return d.zdotdirPath
}

func (d *Daemon) setupManagedZdotdir() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	// Only meaningful for zsh logins. If the user's shell isn't zsh, the
	// ZDOTDIR is ignored by their shell anyway; skip to avoid surprises.
	if filepath.Base(strings.TrimSpace(os.Getenv("SHELL"))) != "zsh" {
		return ""
	}
	selfBin, err := os.Executable()
	if err != nil {
		return ""
	}
	binDir := filepath.Dir(selfBin)

	stateDir, err := cli.StateDirForInstance(d.cfg.Profile, d.cfg.ConfigPath)
	if err != nil {
		d.logger.Warn("managed zdotdir: resolve state dir failed", "error", err)
		return ""
	}
	dir := filepath.Join(stateDir, "agent-zdotdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.logger.Warn("managed zdotdir: mkdir failed", "dir", dir, "error", err)
		return ""
	}

	userZDOTDIR := os.Getenv("ZDOTDIR")
	if userZDOTDIR == "" {
		if home, err := os.UserHomeDir(); err == nil {
			userZDOTDIR = home
		}
	}

	for name, content := range renderZdotdirFiles(binDir, userZDOTDIR) {
		if err := writeFileAtomic(filepath.Join(dir, name), []byte(content)); err != nil {
			d.logger.Warn("managed zdotdir: write failed", "file", name, "error", err)
			return ""
		}
	}
	return dir
}

// renderZdotdirFiles builds the contents of the managed ZDOTDIR startup files.
// Each managed rc file sources the user's matching real rc (so PATH bits like
// fnm/nvm/go survive), then prepends binDir so it wins over anything the user's
// rc added (notably brew shellenv). .zshenv runs for every zsh; .zprofile for
// login; .zshrc for interactive. We prepend in all three so binDir leads
// regardless of how the agent invokes zsh (-c / -lc / -ilc).
func renderZdotdirFiles(binDir, userZDOTDIR string) map[string]string {
	prepend := fmt.Sprintf("export PATH=%s:$PATH\n", shellSingleQuote(binDir))
	return map[string]string{
		".zshenv":   sourceUserRC(userZDOTDIR, ".zshenv") + prepend,
		".zprofile": sourceUserRC(userZDOTDIR, ".zprofile") + prepend,
		".zshrc":    sourceUserRC(userZDOTDIR, ".zshrc") + prepend,
		".zlogin":   sourceUserRC(userZDOTDIR, ".zlogin"),
	}
}

// sourceUserRC emits a guarded `source` line for the user's real rc file. zsh
// reads ZDOTDIR-relative startup files; without re-sourcing the user's own, the
// agent would lose every PATH/env line they rely on.
func sourceUserRC(userZDOTDIR, name string) string {
	if userZDOTDIR == "" {
		return ""
	}
	p := filepath.Join(userZDOTDIR, name)
	return fmt.Sprintf("[ -f %s ] && source %s\n", shellSingleQuote(p), shellSingleQuote(p))
}

// shellSingleQuote wraps s in single quotes, escaping embedded single quotes,
// so it is safe to inline into a generated shell script.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeFileAtomic writes via a temp file + rename so a concurrently spawning
// agent never sources a half-written rc file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-zdotdir-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
