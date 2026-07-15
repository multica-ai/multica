//go:build windows

package agent

import "testing"

func TestCodexInitializeRetrySuppressedWithoutConfirmedTreeCleanup(t *testing.T) {
	if codexInitializeRetrySupported() {
		t.Fatal("Codex initialize retry must remain disabled until Windows descendant cleanup is positively confirmed")
	}
}
