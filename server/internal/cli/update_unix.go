//go:build !windows

package cli

import "os"

// replaceBinary swaps the running executable for the freshly-downloaded one.
// On Unix, the kernel keeps the old inode alive for the running process, so a
// plain rename is safe.
func replaceBinary(tmpPath, exePath string) error {
	return os.Rename(tmpPath, exePath)
}

// CleanupStaleUpdateArtifacts is a no-op on Unix — there are no sidecar files
// to reclaim.
func CleanupStaleUpdateArtifacts() {}
