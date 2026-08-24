//go:build windows

package daemon

// diskFreePercent is not implemented on Windows — returns nil (unknown) and
// the server skips the pressure warning. See diskpressure_unix.go.
func diskFreePercent(path string) *float64 { return nil }
