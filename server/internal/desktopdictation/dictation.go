// Package desktopdictation owns the Desktop-only, fixed global dictation chord.
// It has no daemon, account, audio, credential, or network dependency.
package desktopdictation

import "strconv"

// HelperArg is private to the matching bundled Desktop client. Version the
// protocol rather than falling back to arbitrary commands or an older CLI.
const HelperArg = "--desktop-dictation-v1"

type keyEvent struct {
	key uint16
	up  bool
}

type platform interface {
	available() bool
	desktopRunning() bool
	foregroundWindow() uintptr
	keyDown(uint16) bool
	sendKeys([]keyEvent) int
}

var guardedKeys = [...]uint16{
	0x10, 0x11, 0x12, // Shift, Ctrl, Alt (either side).
	0x5B, 0x5C, // Windows keys.
	0x44, 0x0D, 0x20, // D, Enter, Space; never combine with an active submit key.
}

// Run returns only a bounded status token. "probe" is read-only; "toggle" takes
// exactly one decimal HWND chosen by Electron main, never a key or script.
// Electron owns trusted-frame/user-gesture admission. This helper rechecks the
// actual foreground HWND immediately before sending one fixed chord.
func Run(args []string) string {
	return run(args, nativePlatform())
}

func run(args []string, p platform) string {
	probe := len(args) == 1 && args[0] == "probe"
	var hwnd uint64
	if !probe {
		if len(args) != 2 || args[0] != "toggle" {
			return "unavailable"
		}
		var err error
		hwnd, err = strconv.ParseUint(args[1], 10, strconv.IntSize)
		if err != nil || hwnd == 0 || strconv.FormatUint(hwnd, 10) != args[1] {
			return "unavailable"
		}
	}
	if p == nil || !p.available() {
		return "unavailable"
	}
	if !probe && p.foregroundWindow() != uintptr(hwnd) {
		return "not_focused"
	}
	if !p.desktopRunning() {
		return "app_not_running"
	}
	if probe {
		return "ready"
	}
	for _, key := range guardedKeys {
		if p.keyDown(key) {
			return "busy"
		}
	}
	// Process enumeration can take time. Do not steal focus or send into a
	// different application if the user moved away in the meantime.
	if p.foregroundWindow() != uintptr(hwnd) {
		return "not_focused"
	}
	chord := []keyEvent{
		{key: 0x11}, {key: 0x12}, {key: 0x10}, {key: 0x44},
		{key: 0x44, up: true}, {key: 0x10, up: true}, {key: 0x12, up: true}, {key: 0x11, up: true},
	}
	sent := p.sendKeys(chord)
	if sent == len(chord) {
		return "sent"
	}
	if sent > 0 && sent < len(chord) {
		// No retry: a partially sent toggle may already have started dictation.
		// Release only keys left down by the inserted prefix, in reverse order.
		var held []uint16
		for _, event := range chord[:sent] {
			if event.up {
				held = held[:len(held)-1]
			} else {
				held = append(held, event.key)
			}
		}
		release := make([]keyEvent, 0, len(held))
		for i := len(held) - 1; i >= 0; i-- {
			release = append(release, keyEvent{key: held[i], up: true})
		}
		if p.sendKeys(release) != len(release) {
			return "cleanup_failed"
		}
	}
	return "unavailable"
}
