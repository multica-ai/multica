package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestNewWriterLoggerDefault verifies that both the returned component logger
// and bare slog.* calls write to the supplied writer (not os.Stderr), with the
// component tag attached and color escapes disabled. This is the contract the
// daemon relies on to funnel every log line into its rotating daemon.log.
func TestNewWriterLoggerDefault(t *testing.T) {
	// slog.SetDefault mutates global state, so this test cannot run in parallel
	// with others touching the default logger; restore it afterwards.
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	log := NewWriterLoggerDefault("daemon", &buf)

	log.Error("boom", "code", 42)
	slog.Warn("global-line")

	out := buf.String()
	if !strings.Contains(out, "boom") {
		t.Errorf("component logger output missing message: %q", out)
	}
	if !strings.Contains(out, "component=daemon") {
		t.Errorf("output missing component tag: %q", out)
	}
	if !strings.Contains(out, "global-line") {
		t.Errorf("global slog.Warn did not reach the writer: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output contains ANSI color escapes, want NoColor: %q", out)
	}
}
