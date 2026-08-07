package agent

import (
	"context"
	"strings"
)

type platformAgentBackend struct {
	transport *codexBackend
}

func newPlatformAgentBackend(cfg Config) *platformAgentBackend {
	cfg.CodexVersion = ""
	cfg.Env = cloneStringMap(cfg.Env)
	for key := range cfg.Env {
		if strings.EqualFold(key, "CODEX_HOME") {
			delete(cfg.Env, key)
		}
	}
	return &platformAgentBackend{
		transport: &codexBackend{cfg: cfg, policy: &platformAgentAppServerPolicy},
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func platformAgentExecOptions(opts ExecOptions) ExecOptions {
	return platformAgentAppServerPolicy.execOptions(opts)
}

func (b *platformAgentBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	return b.transport.Execute(ctx, prompt, platformAgentExecOptions(opts))
}
