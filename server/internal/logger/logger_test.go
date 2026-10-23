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

// TestColorEnabled pins the LOG_COLOR contract: unset means auto (follow
// terminal detection), an explicit strconv.ParseBool value overrides it, and
// an unparsable value falls back to auto. This is what lets a TTY-allocated
// backend container emit plain-text logs via LOG_COLOR=false.
func TestColorEnabled(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		term bool
		want bool
	}{
		{"unset follows terminal", "", true, true},
		{"unset follows non-terminal", "", false, false},
		{"true overrides non-terminal", "true", false, true},
		{"1 overrides non-terminal", "1", false, true},
		{"false overrides terminal", "false", true, false},
		{"0 overrides terminal", "0", true, false},
		{"mixed case and spaces", "  True  ", false, true},
		{"invalid falls back to terminal", "yes", true, true},
		{"invalid falls back to non-terminal", "never", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := colorEnabled(tc.raw, tc.term); got != tc.want {
				t.Errorf("colorEnabled(%q, %v) = %v, want %v", tc.raw, tc.term, got, tc.want)
			}
		})
	}
}
