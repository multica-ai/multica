//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// autostartRunKeyPath is the per-user Run key. It is a variable purely so a
// test can point registration at a throwaway subkey instead of writing the
// developer's real login entries.
var autostartRunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func platformAutostartSupported() bool { return true }

// platformWriteAutostart sets the profile's Run value. The HKCU hive needs
// no elevation, and the value is rewritten only when the command line
// actually changed — a no-op refresh on every `daemon start`.
func platformWriteAutostart(profile string, spec autostartSpec) (autostartState, bool, error) {
	name := windowsRunValueName(profile)
	command := windowsAutostartCommand(spec)

	// QUERY_VALUE rides along with SET_VALUE because the idempotence check
	// below reads the current value through this same handle — a handle
	// opened write-only denies GetStringValue with ERROR_ACCESS_DENIED.
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return autostartState{}, false, err
	}
	defer key.Close()

	previous, _, err := key.GetStringValue(name)
	changed := errors.Is(err, registry.ErrNotExist) || previous != command
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return autostartState{}, false, err
	}
	if changed {
		if err := key.SetStringValue(name, command); err != nil {
			return autostartState{}, false, err
		}
	}
	return autostartState{
		Enabled:   true,
		Mechanism: autostartMechanismWindowsRun,
		Location:  `HKCU\` + autostartRunKeyPath + `\` + name,
		Command:   command,
	}, changed, nil
}

// platformRemoveAutostart deletes the profile's Run value. Removing a value
// that is not there reports changed=false so `disable` can say "not enabled"
// rather than erroring on an idempotent cleanup.
func platformRemoveAutostart(profile string) (autostartState, bool, error) {
	name := windowsRunValueName(profile)

	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return autostartState{Mechanism: autostartMechanismWindowsRun}, false, nil
		}
		return autostartState{}, false, err
	}
	defer key.Close()

	err = key.DeleteValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return autostartState{
			Mechanism: autostartMechanismWindowsRun,
			Location:  `HKCU\` + autostartRunKeyPath + `\` + name,
		}, false, nil
	}
	if err != nil {
		return autostartState{}, false, err
	}
	return autostartState{
		Mechanism: autostartMechanismWindowsRun,
		Location:  `HKCU\` + autostartRunKeyPath + `\` + name,
	}, true, nil
}

// platformReadAutostart reports the Run value. A missing key or value reads
// as "disabled" (with the location `enable` would use), not as an error —
// status must answer for a machine that never registered anything.
func platformReadAutostart(profile string) (autostartState, error) {
	name := windowsRunValueName(profile)
	location := `HKCU\` + autostartRunKeyPath + `\` + name

	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return autostartState{Mechanism: autostartMechanismWindowsRun, Location: location}, nil
		}
		return autostartState{}, err
	}
	defer key.Close()

	command, _, err := key.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return autostartState{Mechanism: autostartMechanismWindowsRun, Location: location}, nil
	}
	if err != nil {
		return autostartState{}, err
	}
	return autostartState{
		Enabled:   true,
		Mechanism: autostartMechanismWindowsRun,
		Location:  location,
		Command:   command,
	}, nil
}
