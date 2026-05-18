//go:build windows

package main

import "os"

func watchTerminalResize(ptmx *os.File) func() {
	// Terminal resize watching is not supported on Windows CLI builds.
	return func() {}
}

func makeStdinRaw() (func(), error) {
	// Terminal raw mode is not supported on Windows CLI builds.
	return func() {}, nil
}

func isTerminal(fd int) bool {
	return false
}
