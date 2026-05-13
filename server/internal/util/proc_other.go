//go:build !windows

package util

// EnsureHiddenConsole is a no-op on non-Windows platforms.
func EnsureHiddenConsole() {}
