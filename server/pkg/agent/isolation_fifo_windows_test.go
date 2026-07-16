//go:build windows

package agent

import "testing"

func assertFIFOReadOnlyFileRejected(t *testing.T, writable string) {
	t.Helper()
}
