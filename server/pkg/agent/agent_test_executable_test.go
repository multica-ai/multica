package agent

import (
	"path/filepath"
	"testing"
)

func missingAgentExecutable(tb testing.TB, name string) string {
	tb.Helper()
	return filepath.Join(tb.TempDir(), name)
}
