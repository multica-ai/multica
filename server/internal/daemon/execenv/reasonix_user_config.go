package execenv

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Finding the runtime owner's Reasonix config is not a best-effort lookup: the
// per-task reasonix.toml replaces whatever [permissions] the owner set, so a
// config the daemon fails to find is a deny list the task silently drops. The
// file has to be the one the Reasonix child will actually load, on every
// platform and for every way the owner can move it.
//
// So the resolution below is a port of Reasonix's own (internal/config/paths.go
// in DeepSeek-Reasonix, v2), not an approximation of it:
//
//   - REASONIX_HOME wins, after ${VAR} / ${VAR:-default} expansion, ~ handling,
//     and absolute-path normalization.
//   - Without it, the home is %AppData%\reasonix on Windows (falling back to
//     %USERPROFILE%\AppData\Roaming\reasonix) and ~/.reasonix everywhere else.
//   - config.toml under that home is the primary path. When it does not exist
//     AND REASONIX_HOME is unset, Reasonix reads the legacy OS-support and XDG
//     locations instead — so the daemon has to look there too.
//
// Divergence here is a security bug, not a cosmetic one. Keep this in step with
// upstream.

// reasonixGOOS is runtime.GOOS, overridable so tests can cover the Windows
// resolution from any runner. Never assigned outside tests.
var reasonixGOOS = runtime.GOOS

// reasonixVarRef matches ${VAR} and ${VAR:-default}, mirroring Reasonix's
// ExpandVars.
var reasonixVarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// reasonixEnv is the environment the task's Reasonix child will see: the
// agent's custom_env (already stripped of daemon-blocklisted keys) layered over
// the daemon's own environment, the way the agent process builds its child env.
//
// Presence is what makes this a map rather than a single home string: an agent
// that sets REASONIX_HOME="" has overridden it to empty, which sends the child
// back to the platform default — reading the daemon's own REASONIX_HOME there
// would point at a different config than the one the task runs against.
type reasonixEnv map[string]string

// lookup resolves one variable the way the child would: the agent's override
// when it declared one, else the daemon's own environment.
func (e reasonixEnv) lookup(name string) string {
	if v, ok := e[name]; ok {
		return v
	}
	if reasonixGOOS == "windows" {
		// Windows environment names are case-insensitive, and so is os.Getenv
		// there; an agent that wrote "APPDATA" still overrides "AppData".
		for k, v := range e {
			if strings.EqualFold(k, name) {
				return v
			}
		}
	}
	return os.Getenv(name)
}

// expandVars substitutes ${VAR} / ${VAR:-default}. An unset OR empty variable
// takes the default, matching Reasonix.
func (e reasonixEnv) expandVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return reasonixVarRef.ReplaceAllStringFunc(s, func(match string) string {
		groups := reasonixVarRef.FindStringSubmatch(match)
		if value := e.lookup(groups[1]); value != "" {
			return value
		}
		if groups[2] != "" {
			return groups[3]
		}
		return ""
	})
}

// cleanDir reads a directory-valued variable the way Reasonix's cleanEnvDir
// does: trim, expand, resolve a leading ~, make absolute, clean.
func (e reasonixEnv) cleanDir(name string) string {
	dir := strings.TrimSpace(e.lookup(name))
	if dir == "" {
		return ""
	}
	dir = e.expandVars(dir)
	if dir == "~" {
		if home := e.userHomeDir(); home != "" {
			dir = home
		}
	} else if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home := e.userHomeDir(); home != "" {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Clean(dir)
}

// userHomeDir ports os.UserHomeDir against the child's env.
func (e reasonixEnv) userHomeDir() string {
	if reasonixGOOS == "windows" {
		return e.lookup("USERPROFILE")
	}
	return e.lookup("HOME")
}

// userConfigDir ports os.UserConfigDir against the child's env.
func (e reasonixEnv) userConfigDir() string {
	switch reasonixGOOS {
	case "windows":
		return e.lookup("AppData")
	case "darwin", "ios":
		home := e.userHomeDir()
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support")
	default:
		if dir := e.lookup("XDG_CONFIG_HOME"); dir != "" {
			if !filepath.IsAbs(dir) {
				return "" // os.UserConfigDir rejects a relative XDG_CONFIG_HOME
			}
			return dir
		}
		home := e.userHomeDir()
		if home == "" {
			return ""
		}
		return filepath.Join(home, ".config")
	}
}

// isolatedHome is Reasonix's IsolatedHomeDir: a non-empty REASONIX_HOME marks a
// self-contained runtime, which switches off every legacy fallback below.
func (e reasonixEnv) isolatedHome() string {
	return e.cleanDir("REASONIX_HOME")
}

// homeDir is Reasonix's reasonixHomeDir.
func (e reasonixEnv) homeDir() string {
	if dir := e.isolatedHome(); dir != "" {
		return dir
	}
	if reasonixGOOS == "windows" {
		if dir := e.userConfigDir(); dir != "" {
			return filepath.Join(dir, "reasonix")
		}
		if home := e.userHomeDir(); home != "" {
			return filepath.Join(home, "AppData", "Roaming", "reasonix")
		}
		return ""
	}
	if home := e.userHomeDir(); home != "" {
		return filepath.Join(home, ".reasonix")
	}
	if dir := e.userConfigDir(); dir != "" {
		return filepath.Join(dir, "reasonix")
	}
	return ""
}

// userConfigPath is the primary config.toml under the Reasonix home.
func (e reasonixEnv) userConfigPath() string {
	dir := e.homeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, reasonixUserConfigFile)
}

// legacyOSSupportDir is the pre-Reasonix-home OS app-support directory, empty
// when REASONIX_HOME isolates the runtime or when it is the home already.
func (e reasonixEnv) legacyOSSupportDir() string {
	if e.isolatedHome() != "" {
		return ""
	}
	dir := e.userConfigDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "reasonix")
	if current := e.homeDir(); current != "" && reasonixSamePath(path, current) {
		return ""
	}
	return path
}

// legacyUserConfigPath is the legacy OS app-support config.toml.
func (e reasonixEnv) legacyUserConfigPath() string {
	dir := e.legacyOSSupportDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, reasonixUserConfigFile)
	if primary := e.userConfigPath(); primary != "" && reasonixSamePath(path, primary) {
		return ""
	}
	return path
}

// legacyXDGConfigPaths are the deprecated XDG locations, Unix-only and only
// while REASONIX_HOME is unset.
func (e reasonixEnv) legacyXDGConfigPaths() []string {
	if e.isolatedHome() != "" || reasonixGOOS == "windows" {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if dir := e.cleanDir("XDG_CONFIG_HOME"); dir != "" {
		add(filepath.Join(dir, "reasonix", reasonixUserConfigFile))
	}
	if home := e.userHomeDir(); home != "" {
		add(filepath.Join(home, ".config", "reasonix", reasonixUserConfigFile))
	}
	return paths
}

// userConfigLoadPath is the config.toml the child will read: the primary path
// when it exists, else the first legacy location that does, else the primary
// path again (which simply does not exist yet). Ports userConfigLoadPath.
func (e reasonixEnv) userConfigLoadPath() string {
	primary := e.userConfigPath()
	if primary == "" {
		return e.legacyUserConfigPath()
	}
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	if legacy := e.legacyUserConfigPath(); legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	for _, legacy := range e.legacyXDGConfigPaths() {
		if legacy == "" || reasonixSamePath(legacy, primary) {
			continue
		}
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

func reasonixSamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if abs, err := filepath.Abs(a); err == nil {
		a = abs
	}
	if abs, err := filepath.Abs(b); err == nil {
		b = abs
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
