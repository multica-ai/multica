//go:build windows

package agent

import (
	"context"
	"fmt"
)

// claudeTUIBackend is not supported on Windows: creack/pty has no Windows
// ConPTY implementation in the version we depend on. Calling Execute returns
// an error explaining the platform constraint.
type claudeTUIBackend struct {
	cfg Config
}

func (b *claudeTUIBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	return nil, fmt.Errorf("claude-tui backend is not supported on Windows")
}
