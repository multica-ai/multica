package agent

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
)

const agentStderrTailBytes = 16 * 1024

type stderrTail struct {
	mu    sync.Mutex
	buf   []byte
	limit int
	out   io.Writer
}

func newStderrTail(out io.Writer, limit int) *stderrTail {
	return &stderrTail{out: out, limit: limit}
}

func (t *stderrTail) Write(p []byte) (int, error) {
	if t.out != nil {
		_, _ = t.out.Write(p)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.limit:]...)
	}
	return len(p), nil
}

func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(bytes.TrimSpace(t.buf)))
}

func withAgentStderr(base string, tail *stderrTail) string {
	if tail == nil || tail.String() == "" {
		return base
	}
	return fmt.Sprintf("%s; stderr: %s", base, tail.String())
}
