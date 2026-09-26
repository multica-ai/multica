//go:build !windows && !darwin && !linux

package main

// Boot autostart has no registration mechanism on this platform. The four
// functions exist so the package still builds (and `daemon start` still
// works) on every GOOS the toolchain accepts; shouldRegisterAutostart skips
// silently there, while the explicit `daemon autostart` commands report that
// there is nothing to do.

func platformAutostartSupported() bool { return false }

func platformWriteAutostart(string, autostartSpec) (autostartState, bool, error) {
	return autostartState{Mechanism: autostartMechanismUnsupported}, false, nil
}

func platformRemoveAutostart(string) (autostartState, bool, error) {
	return autostartState{Mechanism: autostartMechanismUnsupported}, false, nil
}

func platformReadAutostart(string) (autostartState, error) {
	return autostartState{Mechanism: autostartMechanismUnsupported}, nil
}
