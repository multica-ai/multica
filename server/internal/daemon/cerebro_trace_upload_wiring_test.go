package daemon

// CEREBRO-PATCH(daemon-trace-upload-wiring-guard): FIR-2757 regression guard.

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonRunStartsTraceUpload(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "d.startTraceUpload(ctx)") {
		t.Fatal("Daemon.Run must start trace upload; an upstream sync likely removed the Cerebro wiring")
	}
}
