//go:build unix

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPiExecuteReportsRefusedResume pins the backend half of the GH #8082 fix.
//
// When a resumed transcript names a working directory that no longer exists,
// Pi prints its refusal to stderr and exits 1 before emitting a single JSON
// event. Without a positive Result.ResumeRejected the daemon sees only
// "exit status 1", classifies a non-retryable process_failure, records the same
// session id again, and the next claim serves the same stale pointer — the
// permanent crash loop the issue reported. This test drives the real backend
// against a fake `pi` that reproduces that exact exit.
func TestPiExecuteReportsRefusedResume(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name              string
		resume            bool
		wantResumeRejects bool
	}{
		{name: "resumed run", resume: true, wantResumeRejects: true},
		// A cold run cannot have had a resume refused, whatever the CLI says.
		{name: "cold run", resume: false, wantResumeRejects: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			sessionPath := filepath.Join(base, "session.jsonl")
			if err := os.WriteFile(sessionPath, []byte(`{"type":"session","cwd":"/gone"}`+"\n"), 0o644); err != nil {
				t.Fatalf("create session: %v", err)
			}

			// Mirrors Pi's own output: the refusal on stderr, nothing on
			// stdout, exit 1.
			script := fmt.Sprintf(
				"#!/bin/sh\ncat >/dev/null\necho %q >&2\nexit 1\n",
				piResumeRefusedMarker+": /gone",
			)
			fakePath := filepath.Join(base, "pi")
			writeTestExecutable(t, fakePath, []byte(script))

			backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("New(pi): %v", err)
			}

			opts := ExecOptions{Timeout: 30 * time.Second, Cwd: base}
			if test.resume {
				opts.ResumeSessionID = sessionPath
			}
			session, err := backend.Execute(t.Context(), "hello", opts)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			for range session.Messages {
			}
			result := <-session.Result

			if result.Status != "failed" {
				t.Fatalf("status = %q, want failed", result.Status)
			}
			if result.ResumeRejected != test.wantResumeRejects {
				t.Fatalf("ResumeRejected = %v, want %v (error=%q)",
					result.ResumeRejected, test.wantResumeRejects, result.Error)
			}
			// The transient flag means "another run holds the transcript" and
			// must stay off: unlike a busy lock, this session never becomes
			// usable again, so it has to be retired rather than retried onto.
			if result.ResumeRejectedTransient {
				t.Fatal("ResumeRejectedTransient set for a permanent refusal")
			}
		})
	}
}

// TestPiStderrWatcherSpansWrites guards the one way the marker could be missed:
// stderr arriving in fragments. A substring test over a single Write would pass
// the test above and still fail in production on a split flush.
func TestPiStderrWatcherSpansWrites(t *testing.T) {
	t.Parallel()

	w := newPiStderrWatcher(io.Discard)
	if w.resumeRefused() {
		t.Fatal("empty stderr reported a refusal")
	}
	marker := piResumeRefusedMarker
	split := len(marker) / 2
	if _, err := w.Write([]byte(marker[:split])); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.resumeRefused() {
		t.Fatal("half the marker reported a refusal")
	}
	if _, err := w.Write([]byte(marker[split:] + ": /gone\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !w.resumeRefused() {
		t.Fatal("marker split across writes was not detected")
	}
}

// TestPiStderrWatcherBoundsTail keeps a chatty run from retaining unbounded
// stderr, while still detecting a marker that arrives after the noise.
func TestPiStderrWatcherBoundsTail(t *testing.T) {
	t.Parallel()

	w := newPiStderrWatcher(io.Discard)
	noise := make([]byte, piStderrTailLimit*3)
	for i := range noise {
		noise[i] = 'x'
	}
	if _, err := w.Write(noise); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := len(w.tail); got > piStderrTailLimit {
		t.Fatalf("retained %d bytes, want <= %d", got, piStderrTailLimit)
	}
	if _, err := w.Write([]byte(piResumeRefusedMarker + ": /gone\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !w.resumeRefused() {
		t.Fatal("marker after bounded noise was not detected")
	}
}
