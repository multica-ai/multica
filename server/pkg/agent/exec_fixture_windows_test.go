//go:build windows

package agent

import (
	"os"
	"testing"
)

func writeTestExecutable(tb testing.TB, path string, content []byte) {
	tb.Helper()
	if err := os.WriteFile(path, content, 0o755); err != nil {
		tb.Fatalf("write test executable %s: %v", path, err)
	}
}
