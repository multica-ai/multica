//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A brew upgrade that hangs is stopped at the deadline, so a daemon can promise
// the server that its installer is no longer running after it.
func TestUpdateViaBrewWithTimeoutStopsAHungBrew(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "finished")
	script := "#!/bin/sh\nsleep 30\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	start := time.Now()
	_, err := UpdateViaBrewWithTimeout(200 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("err = %v, want the deadline failure", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("returned after %s; brew was not stopped", elapsed)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the hung brew ran to completion")
	}
}
