package agent

import (
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/pkg/redact"
)

// agentStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when an agent CLI exits before emitting a structured
// error (e.g. V8 abort on Windows, Bun panic, OOM). Large enough to
// contain typical CLI error lines, small enough to stay sensible inside
// a task-level Result.Error string.
const agentStderrTailBytes = 2048

var (
	agentAuthorizationHeaderRe = regexp.MustCompile(`(?im)(authorization\s*:\s*)[^\r\n]+`)
	agentJSONSecretRe          = regexp.MustCompile(`(?i)("(?:token|auth|authorization|api[_-]?key|secret|password)"\s*:\s*)"(?:\\.|[^"\\])*"`)
	agentDiagnosticSecretRe    = regexp.MustCompile(`(?i)(authorization|auth|api[_-]?key|token|secret|password)(\s*[:=]\s*)([^\s,;]+)`)
)

// sanitizeAgentDiagnostic removes terminal control characters, common secret
// shapes, and local home-directory details before a child-process diagnostic is
// persisted in Result.Error. stderr is still forwarded to the local daemon log;
// this helper protects the task row and user-visible failure comment.
func sanitizeAgentDiagnostic(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = agentAuthorizationHeaderRe.ReplaceAllString(value, `$1[REDACTED]`)
	value = agentJSONSecretRe.ReplaceAllString(value, `$1"[REDACTED]"`)
	value = agentDiagnosticSecretRe.ReplaceAllString(value, `$1$2[REDACTED]`)
	return redact.Text(value)
}

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

	mu    sync.Mutex
	buf   []byte
	total int64
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
	s.total += int64(len(p))
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	s.mu.Unlock()
	return len(p), nil
}

func (s *stderrTail) TotalBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// Tail returns the captured stderr as valid UTF-8 with leading/trailing
// whitespace trimmed. A byte-bounded tail can start or end partway through a
// multi-byte rune, so invalid fragments are discarded before the diagnostic is
// persisted. The inner writer still receives every original byte verbatim.
// Empty string means nothing was written or no valid non-whitespace text was
// captured.
func (s *stderrTail) Tail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(strings.ToValidUTF8(string(s.buf), ""))
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
