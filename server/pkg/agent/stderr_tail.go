package agent

import (
	"io"
	"strings"
	"sync"
)

// agentStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when an agent CLI exits before emitting a structured
// error (e.g. V8 abort on Windows, Bun panic, OOM). Large enough to
// contain typical CLI error lines, small enough to stay sensible inside
// a task-level Result.Error string.
const agentStderrTailBytes = 2048

// stderrTail forwards writes to an inner writer (typically the daemon's
// log) while also retaining a bounded tail of the bytes written. Consumers
// call Tail() to include that context in error messages when the agent
// process exits before it emits a structured error — otherwise all the
// user sees is "exit status N", with the real reason stuck in daemon logs.
//
// All backends that supervise a child CLI process should wire their
// cmd.Stderr through this type, and on failure include Tail() in
// Result.Error via withAgentStderr. That makes root-causing CLI crashes
// possible without having to crawl the daemon host's log files.
type stderrTail struct {
	inner io.Writer
	max   int

	mu  sync.Mutex
	buf []byte
}

func newStderrTail(inner io.Writer, max int) *stderrTail {
	if max <= 0 {
		max = agentStderrTailBytes
	}
	return &stderrTail{inner: inner, max: max}
}

func (s *stderrTail) Write(p []byte) (int, error) {
	if _, err := s.inner.Write(p); err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	s.mu.Unlock()
	return len(p), nil
}

// Tail returns the captured stderr with leading/trailing whitespace
// trimmed; empty string means nothing was written or everything was
// whitespace.
func (s *stderrTail) Tail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(s.buf))
}

// withAgentStderr appends a stderr tail hint to an error message when
// non-empty, otherwise returns msg unchanged. The tail is prefixed with a
// short label so the composed string stays readable even when the original
// msg is already verbose.
func withAgentStderr(msg, label, tail string) string {
	if tail == "" {
		return msg
	}
	return msg + "; " + label + " stderr: " + tail
}
