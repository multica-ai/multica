package main

import (
	"strings"
	"testing"
	"time"
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

func TestUpdateCommandRegistersDownloadTimeoutFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("download-timeout")
	if flag == nil {
		t.Fatal("updateCmd is missing --download-timeout")
	}
	if got := flag.DefValue; got != (120 * time.Second).String() {
		t.Fatalf("--download-timeout default = %q, want %q", got, (120 * time.Second).String())
	}
}
