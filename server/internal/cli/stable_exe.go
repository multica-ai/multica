package cli

import (
	"path/filepath"
	"strings"
)

// StableExecutablePath maps a self-executable path to the path that should be
// recorded wherever a multica invocation has to outlive a brew upgrade — the
// daemon's binary-update restart target and a boot-autostart entry's
// ExecStart/ProgramArguments.
//
// The two consumers must never disagree: `os.Executable()` reports the
// versioned keg path on brew installs (`/proc/self/exe` on Linux, realpath on
// macOS), and `brew upgrade` deletes that path when it cleans the old keg.
// A unit recorded with the keg path survives neither the restart it would
// trigger (systemd fails ExecStart with 203/EXEC and gives up after
// StartLimitBurst) nor the next login (a LaunchAgent pointing at a deleted
// file), while the spawn-based restart handoff has always resolved through
// the same brew facts — which is exactly why this mapping lives in one
// function both callers use.
//
// brewInstall and brewPrefix are the caller's detection facts:
//
//	brewInstall=true,  prefix!=""  -> <prefix>/bin/multica (stable brew symlink)
//	brewInstall=true,  prefix==""  -> path unchanged; the caller decides how to
//	                                  report a brew install whose prefix it
//	                                  could not resolve
//	brewInstall=false              -> path with symlinks resolved, or path
//	                                  unchanged when resolution fails
//
// Detection itself stays with the caller because the daemon caches it per
// process behind its seams (see Daemon.restartTargetBinary) while one-shot
// callers use StableSelfExecutable. Both detect in the SAME order — the
// path's own shape (a known Cellar) first, then `brew --prefix` — so a keg
// path always resolves to ITS prefix and the two can never drift.
func StableExecutablePath(path, brewPrefix string, brewInstall bool) string {
	if brewInstall {
		if brewPrefix != "" {
			// filepath.Join, as the daemon has always done: brew only exists
			// on unix where the separators coincide anyway, and matching the
			// historical mapping keeps every existing expectation intact.
			return filepath.Join(brewPrefix, "bin", "multica")
		}
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// StableSelfExecutable is the one-stop form of StableExecutablePath for
// callers that hold a self-executable path and no brew-fact cache of their
// own (boot-autostart registration). Detection order mirrors the daemon's —
// MatchKnownBrewPrefix first, GetBrewPrefix as fallback — so both consumers
// resolve the same path for the same install.
func StableSelfExecutable(path string) string {
	resolved := evalSymlinkOrSelf(path)
	// The path's own shape is the stronger evidence: a known Cellar path
	// resolves to THAT Cellar's prefix even if `brew --prefix` reports a
	// different root, so fixtures and moved installs stay per-path correct.
	if prefix := MatchKnownBrewPrefix(resolved); prefix != "" {
		return StableExecutablePath(path, prefix, true)
	}
	if prefix := GetBrewPrefix(); prefix != "" && strings.HasPrefix(resolved, prefix) {
		return StableExecutablePath(path, prefix, true)
	}
	return StableExecutablePath(path, "", false)
}

// brewInstallForPath reports whether path belongs to a Homebrew install,
// using the same checks and order as StableSelfExecutable but answering a
// yes/no question — IsBrewInstall uses it against the running binary, and
// StableSelfExecutable's fallback containment check mirrors it.
func brewInstallForPath(path string) bool {
	resolved := evalSymlinkOrSelf(path)
	if MatchKnownBrewPrefix(resolved) != "" {
		return true
	}
	prefix := GetBrewPrefix()
	return prefix != "" && strings.HasPrefix(resolved, prefix)
}

// evalSymlinkOrSelf resolves symlinks when it can and returns the input
// unchanged otherwise — a path that does not exist (a stale keg, a fixture)
// must still be shape-matchable against the known prefixes.
func evalSymlinkOrSelf(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
