package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

// setReasonixGOOS pins the platform the resolver targets so the Windows and
// macOS paths are covered from any runner. These tests are deliberately NOT
// parallel: Go resumes parallel tests only once every serial test has finished,
// so the global stays consistent for the parallel reasonix tests.
func setReasonixGOOS(t *testing.T, goos string) {
	t.Helper()
	previous := reasonixGOOS
	reasonixGOOS = goos
	t.Cleanup(func() { reasonixGOOS = previous })
}

// TestReasonixUserConfigPathWindows pins the Windows resolution: %AppData% is
// the home root, NOT %USERPROFILE%\.reasonix. Reading the wrong file there means
// reading no owner permissions at all, and the task config would then replace
// the rules the child actually loads.
func TestReasonixUserConfigPathWindows(t *testing.T) {
	setReasonixGOOS(t, "windows")

	appData := t.TempDir()
	profile := t.TempDir()
	env := reasonixEnv{"REASONIX_HOME": "", "AppData": appData, "USERPROFILE": profile}
	if got, want := env.userConfigLoadPath(), filepath.Join(appData, "reasonix", reasonixUserConfigFile); got != want {
		t.Fatalf("windows config path = %q, want %q", got, want)
	}

	// %AppData% missing: Reasonix falls back to the Roaming path under the
	// profile, still not to a dot-directory.
	env = reasonixEnv{"REASONIX_HOME": "", "AppData": "", "USERPROFILE": profile}
	want := filepath.Join(profile, "AppData", "Roaming", "reasonix", reasonixUserConfigFile)
	if got := env.userConfigLoadPath(); got != want {
		t.Fatalf("windows config path without AppData = %q, want %q", got, want)
	}

	// An agent may write the variable in any case; Windows env lookups do not
	// care, so neither may this one.
	env = reasonixEnv{"REASONIX_HOME": "", "APPDATA": appData, "USERPROFILE": profile}
	if got, want := env.userConfigLoadPath(), filepath.Join(appData, "reasonix", reasonixUserConfigFile); got != want {
		t.Fatalf("windows config path with APPDATA spelling = %q, want %q", got, want)
	}
}

// TestReasonixUserConfigPathHonorsExplicitEmptyOverride is the presence-aware
// case: an agent that sets REASONIX_HOME="" sends its child back to the platform
// default, so the daemon must not keep reading its own REASONIX_HOME.
func TestReasonixUserConfigPathHonorsExplicitEmptyOverride(t *testing.T) {
	setReasonixGOOS(t, "linux")
	daemonHome := t.TempDir()
	t.Setenv("REASONIX_HOME", daemonHome)
	t.Setenv("XDG_CONFIG_HOME", "")

	userHome := t.TempDir()
	cleared := reasonixEnv{"REASONIX_HOME": "", "HOME": userHome}
	if got, want := cleared.userConfigLoadPath(), filepath.Join(userHome, ".reasonix", reasonixUserConfigFile); got != want {
		t.Fatalf("path with an explicitly cleared override = %q, want the platform default %q", got, want)
	}

	// No entry at all means the child inherits the daemon's own variable.
	inherited := reasonixEnv{"HOME": userHome}
	if got, want := inherited.userConfigLoadPath(), filepath.Join(daemonHome, reasonixUserConfigFile); got != want {
		t.Fatalf("path without an override = %q, want the daemon's home %q", got, want)
	}
}

// TestReasonixUserConfigPathExpandsOverride covers the forms Reasonix accepts in
// REASONIX_HOME: ${VAR} / ${VAR:-default}, a leading ~, surrounding whitespace,
// and a relative path.
func TestReasonixUserConfigPathExpandsOverride(t *testing.T) {
	setReasonixGOOS(t, "linux")
	home := t.TempDir()
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for _, tc := range []struct {
		name     string
		override string
		want     string
	}{
		{name: "tilde", override: "~/rx", want: filepath.Join(home, "rx")},
		{name: "variable", override: "${RX_ROOT}/rx", want: filepath.Join(root, "rx")},
		{name: "variable default", override: "${RX_UNSET:-" + root + "/fallback}", want: filepath.Join(root, "fallback")},
		{name: "whitespace", override: "  " + root + "/spaced  ", want: filepath.Join(root, "spaced")},
		{name: "relative", override: "relative-rx", want: filepath.Join(cwd, "relative-rx")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := reasonixEnv{"REASONIX_HOME": tc.override, "HOME": home, "RX_ROOT": root}
			if got, want := env.userConfigLoadPath(), filepath.Join(tc.want, reasonixUserConfigFile); got != want {
				t.Fatalf("config path = %q, want %q", got, want)
			}
		})
	}
}

// TestReasonixUserConfigPathFallsBackToLegacy covers the compatibility lookup:
// with no REASONIX_HOME and no config under the current home, Reasonix reads the
// legacy OS-support location, then the XDG one — and so must the daemon.
func TestReasonixUserConfigPathFallsBackToLegacy(t *testing.T) {
	setReasonixGOOS(t, "darwin")
	ownerConfig := "[permissions]\ndeny = [\"bash\"]\n"

	home := t.TempDir()
	legacyOS := filepath.Join(home, "Library", "Application Support", "reasonix", reasonixUserConfigFile)
	mustWrite(t, legacyOS, ownerConfig)
	env := reasonixEnv{"REASONIX_HOME": "", "HOME": home, "XDG_CONFIG_HOME": ""}
	if got := env.userConfigLoadPath(); got != legacyOS {
		t.Fatalf("config path = %q, want the legacy OS-support config %q", got, legacyOS)
	}

	// Only the XDG copy exists.
	home = t.TempDir()
	legacyXDG := filepath.Join(home, ".config", "reasonix", reasonixUserConfigFile)
	mustWrite(t, legacyXDG, ownerConfig)
	env = reasonixEnv{"REASONIX_HOME": "", "HOME": home, "XDG_CONFIG_HOME": ""}
	if got := env.userConfigLoadPath(); got != legacyXDG {
		t.Fatalf("config path = %q, want the legacy XDG config %q", got, legacyXDG)
	}

	// The primary config wins whenever it exists.
	primary := filepath.Join(home, ".reasonix", reasonixUserConfigFile)
	mustWrite(t, primary, ownerConfig)
	if got := env.userConfigLoadPath(); got != primary {
		t.Fatalf("config path = %q, want the primary config %q", got, primary)
	}
}

// TestReasonixUserConfigPathIsolatesExplicitHome pins Reasonix's isolation rule:
// once REASONIX_HOME is set, no legacy location is consulted, even when one
// holds a config.
func TestReasonixUserConfigPathIsolatesExplicitHome(t *testing.T) {
	setReasonixGOOS(t, "darwin")
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "Library", "Application Support", "reasonix", reasonixUserConfigFile), "[permissions]\ndeny = [\"bash\"]\n")
	mustWrite(t, filepath.Join(home, ".config", "reasonix", reasonixUserConfigFile), "[permissions]\ndeny = [\"bash\"]\n")

	isolated := t.TempDir()
	env := reasonixEnv{"REASONIX_HOME": isolated, "HOME": home, "XDG_CONFIG_HOME": ""}
	if got, want := env.userConfigLoadPath(), filepath.Join(isolated, reasonixUserConfigFile); got != want {
		t.Fatalf("config path = %q, want the isolated home's %q", got, want)
	}
}

// TestReasonixProjectConfigMergesLegacyOwnerConfig is the end-to-end version of
// the fallback: a deny list that only lives in a legacy location still has to
// reach the task config.
func TestReasonixProjectConfigMergesLegacyOwnerConfig(t *testing.T) {
	setReasonixGOOS(t, "darwin")
	home := t.TempDir()
	mustWrite(t,
		filepath.Join(home, "Library", "Application Support", "reasonix", reasonixUserConfigFile),
		"[permissions]\ndeny = [\"bash\"]\n",
	)

	workDir := t.TempDir()
	env := map[string]string{"REASONIX_HOME": "", "HOME": home, "XDG_CONFIG_HOME": ""}
	if err := writeReasonixProjectConfig(workDir, env, &sidecarManifest{}, testLogger()); err != nil {
		t.Fatalf("writeReasonixProjectConfig: %v", err)
	}
	assertTaskDenyList(t, filepath.Join(workDir, reasonixProjectConfigFile), []string{"bash", "ask"})
}
