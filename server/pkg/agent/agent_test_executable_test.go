package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testFallbackTimeout bounds waits that only exist so a broken test fails
// instead of hanging. It is not part of any behavior under test: fixture
// processes can take seconds to start on a loaded machine (macOS scans every
// new executable before its first exec), so a timeout that a fixture has to
// beat must be this one, never a few-second budget.
const testFallbackTimeout = 60 * time.Second

func missingAgentExecutable(tb testing.TB, name string) string {
	tb.Helper()
	return filepath.Join(tb.TempDir(), name)
}

// waitForTestFile blocks until a fixture creates path.
func waitForTestFile(tb testing.TB, path string) {
	tb.Helper()
	deadline := time.Now().Add(testFallbackTimeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("fixture never created %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// expirableContext reports context.DeadlineExceeded once expire is called, so
// a test can end the caller's budget after the fixture reaches a known point
// instead of racing a wall-clock deadline against fixture startup.
type expirableContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newExpirableContext() *expirableContext {
	return &expirableContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *expirableContext) Done() <-chan struct{} { return c.done }

func (c *expirableContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *expirableContext) expire() { c.once.Do(func() { close(c.done) }) }
