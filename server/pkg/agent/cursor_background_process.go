package agent

import (
	"errors"
	"sync"
)

var (
	errCursorBackgroundProcessInvalid  = errors.New("cursor background process is not an owned descendant")
	errCursorBackgroundProcessIdentity = errors.New("cursor background process identity can no longer be proven")
)

// cursorBackgroundProcess is a platform-owned handle for the process Cursor
// reports in a completed shellToolCall. The handle deliberately owns more than
// the reported PID: a PID can be reused after the shell exits.
type cursorBackgroundProcess struct {
	mu       sync.Mutex
	closed   bool
	platform cursorBackgroundProcessPlatform
}

type cursorBackgroundProcessPlatform interface {
	alive() (bool, error)
	terminate() error
	close()
}

func (p *cursorBackgroundProcess) Alive() (bool, error) {
	if p == nil {
		return false, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.platform == nil {
		return false, nil
	}
	return p.platform.alive()
}

func (p *cursorBackgroundProcess) Terminate() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.platform == nil {
		return nil
	}
	return p.platform.terminate()
}

func (p *cursorBackgroundProcess) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.platform != nil {
		p.platform.close()
	}
}
