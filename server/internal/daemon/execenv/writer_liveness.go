package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writerAliveMarker sits in the task workdir for the duration of one run.
// Written when the run starts, removed only on clean completion — so a
// leftover marker means the prior writer died mid-task and the workdir may
// hold half-finished state. Reuse treats its presence as "dirty" and declines,
// forcing the next task through a fresh Prepare. // ponytail: marker existence only; PID check when false positives appear
const writerAliveMarker = ".writer_alive"

func writerAlivePath(workDir string) string {
	return filepath.Join(workDir, writerAliveMarker)
}

// MarkWriterAlive records that a writer is active in workDir. Best-effort
// callers log failures; a missing marker only disables reuse protection.
func MarkWriterAlive(workDir string) error {
	data := []byte(fmt.Sprintf("pid=%d started=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)))
	return os.WriteFile(writerAlivePath(workDir), data, 0o644)
}

// ClearWriterAlive removes the marker after a clean run. Best-effort: a failed
// unlink costs one unnecessary fresh checkout later, never correctness.
func ClearWriterAlive(workDir string) {
	os.Remove(writerAlivePath(workDir))
}

// WriterAliveMarkerPresent reports whether workdir holds an unclean-run marker.
func WriterAliveMarkerPresent(workDir string) bool {
	_, err := os.Stat(writerAlivePath(workDir))
	return err == nil
}
