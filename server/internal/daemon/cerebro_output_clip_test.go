package daemon

import (
	"strings"
	"testing"
)

func TestClipToolOutputKeepsShortOutputIntact(t *testing.T) {
	in := "all good"
	if got := clipToolOutput(in, 8192); got != in {
		t.Fatalf("clipToolOutput(short) = %q, want %q", got, in)
	}
}

func TestClipToolOutputKeepsTheTail(t *testing.T) {
	// The failure line is the last thing a command prints. Head-only
	// truncation discarded exactly the diagnostic part.
	body := strings.Repeat("noise\n", 5000)
	in := body + "FATAL: database connection refused"

	got := clipToolOutput(in, 1024)

	if !strings.Contains(got, "FATAL: database connection refused") {
		t.Fatalf("clipToolOutput dropped the tail; got tail %q", got[max(0, len(got)-80):])
	}
	if !strings.HasPrefix(got, "noise") {
		t.Fatalf("clipToolOutput dropped the head; got head %q", got[:min(40, len(got))])
	}
	if len(got) > 1024 {
		t.Fatalf("clipToolOutput returned %d bytes, want <= 1024", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatal("clipToolOutput must mark the elision so the reader knows bytes are missing")
	}
}

func TestClipToolOutputHandlesTinyLimit(t *testing.T) {
	got := clipToolOutput(strings.Repeat("x", 500), 10)
	if len(got) > 10 {
		t.Fatalf("clipToolOutput returned %d bytes, want <= 10", len(got))
	}
}

func TestClipToolOutputHandlesNonPositiveLimit(t *testing.T) {
	if got := clipToolOutput("anything", 0); got != "" {
		t.Fatalf("clipToolOutput(_, 0) = %q, want empty", got)
	}
}
