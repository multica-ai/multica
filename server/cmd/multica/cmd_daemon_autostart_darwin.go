//go:build darwin

package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func platformAutostartSupported() bool { return true }

// launchAgentsDir is where launchd loads per-user login agents from. Files
// dropped here are picked up at the next login without an explicit
// `launchctl load`, which is what makes the plist the registration itself.
func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func launchAgentPath(profile string) (string, error) {
	dir, err := launchAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, launchAgentLabel(profile)+".plist"), nil
}

// platformWriteAutostart renders (or refreshes) the profile's LaunchAgent.
// The content comparison keeps every `daemon start` after the first a
// read-only no-op unless the executable or PATH snapshot actually changed.
func platformWriteAutostart(profile string, spec autostartSpec) (autostartState, bool, error) {
	label := launchAgentLabel(profile)
	path, err := launchAgentPath(profile)
	if err != nil {
		return autostartState{}, false, err
	}
	content := launchAgentPlistContent(spec, label)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return autostartState{}, false, err
	}
	previous, readErr := os.ReadFile(path)
	changed := readErr != nil || string(previous) != content
	if changed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return autostartState{}, false, err
		}
	}

	// Best-effort `launchctl enable`: it clears a per-label disable someone
	// may have set with `launchctl disable` and does not start anything.
	// Failure is not an error — the plist alone still loads at next login.
	_ = exec.Command("launchctl", "enable", "gui/"+strconv.Itoa(os.Getuid())+"/"+label).Run()

	return autostartState{
		Enabled:   true,
		Mechanism: autostartMechanismLaunchd,
		Location:  path,
		Command:   autostartCommandDisplay(spec),
	}, changed, nil
}

// platformRemoveAutostart deletes the LaunchAgent. If launchd still has the
// label loaded from a prior session it is booted out first so a live entry
// cannot keep running after the user removed its registration.
func platformRemoveAutostart(profile string) (autostartState, bool, error) {
	label := launchAgentLabel(profile)
	path, err := launchAgentPath(profile)
	if err != nil {
		return autostartState{}, false, err
	}

	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return autostartState{
				Mechanism: autostartMechanismLaunchd,
				Location:  path,
			}, false, nil
		}
		return autostartState{}, false, statErr
	}

	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+label).Run()
	if err := os.Remove(path); err != nil {
		return autostartState{}, false, err
	}
	return autostartState{
		Mechanism: autostartMechanismLaunchd,
		Location:  path,
	}, true, nil
}

// platformReadAutostart treats the plist's presence as "enabled" — the file
// in ~/Library/LaunchAgents is what launchd honors at login.
func platformReadAutostart(profile string) (autostartState, error) {
	path, err := launchAgentPath(profile)
	if err != nil {
		return autostartState{}, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return autostartState{Mechanism: autostartMechanismLaunchd, Location: path}, nil
	}
	if err != nil {
		return autostartState{}, err
	}
	return autostartState{
		Enabled:   true,
		Mechanism: autostartMechanismLaunchd,
		Location:  path,
		Command:   launchAgentStoredCommand(content),
	}, nil
}
