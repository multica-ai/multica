package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// mimocodeBackend implements Backend by spawning `mimo acp` and communicating
// via the ACP (Agent Communication Protocol) JSON-RPC 2.0 over stdin/stdout.
// This follows the same pattern as hermesBackend since MiMoCode uses the
// same ACP protocol.
type mimocodeBackend struct {
	cfg Config
}

func (b *mimocodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "mimo"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("mimo executable not found at %q: %w", execPath, err)
	}

	// Reuse the hermes ACP client since MiMoCode uses the same protocol
	hermes := &hermesBackend{cfg: b.cfg}
	return hermes.Execute(ctx, prompt, opts)
}
