package main

import (
	"strings"
	"testing"
)

func TestRunUpdateRejectsNonPositiveDownloadTimeout(t *testing.T) {
	orig := updateDownloadTimeout
	updateDownloadTimeout = 0
	t.Cleanup(func() { updateDownloadTimeout = orig })

	err := runUpdate(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "download timeout must be greater than zero") {
		t.Fatalf("runUpdate error = %v, want download timeout validation", err)
	}
}
