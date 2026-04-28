package agent

import "context"

// codebuddyBackend implements Backend by spawning the CodeBuddy Code CLI
// with --output-format stream-json. The CLI interface is identical to Claude Code,
// so this backend reuses the claude streaming protocol.
type codebuddyBackend struct {
	cfg Config
}

// Execute runs a prompt via CodeBuddy Code. The CodeBuddy CLI accepts the same
// flags as Claude Code (--output-format stream-json, --permission-mode, -p, etc.)
// and emits the same streaming JSON format, so we delegate directly to the
// shared claudeBackend implementation with the executable path swapped.
func (b *codebuddyBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codebuddy"
	}
	delegate := &claudeBackend{
		cfg: Config{
			ExecutablePath: execPath,
			Env:            b.cfg.Env,
			Logger:         b.cfg.Logger,
		},
	}
	return delegate.Execute(ctx, prompt, opts)
}
