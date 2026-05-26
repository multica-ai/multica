package agent

import (
	"context"
)

// codebuddyBackend implements Backend by reusing claudeBackend's stream-json
// pipeline against Tencent CodeBuddy (`cbc`). CodeBuddy's CLI accepts the
// same `-p / --output-format stream-json / --input-format stream-json /
// --permission-mode / --mcp-config / --model / --effort / --max-turns /
// --append-system-prompt / --resume` flags as Claude Code, and emits the
// same `stream-json` envelope shape, so the entire arg/IO machinery in
// claude.go can be delegated verbatim.
//
// The wrapper exists (rather than aliasing "codebuddy" → "claude" in
// agent.New) to:
//   - Keep the provider distinct in runtime listings, usage reports, and
//     daemon logs — observability is aggregated by provider name.
//   - Pin its own model catalog (cbc routes to GLM/Kimi/DeepSeek/MiniMax,
//     not Anthropic) — see codebuddyStaticModels in models.go.
//   - Force ClaudeUseSDKBridge=false: the Anthropic Agent SDK bridge
//     in claudeBackend talks directly to Anthropic with Claude Code's
//     credentials, which is the wrong endpoint and the wrong token for cbc.
//   - Default ExecutablePath to "cbc" instead of "claude".
type codebuddyBackend struct {
	inner *claudeBackend
}

func newCodebuddyBackend(cfg Config) *codebuddyBackend {
	if cfg.ExecutablePath == "" {
		cfg.ExecutablePath = "cbc"
	}
	return &codebuddyBackend{inner: &claudeBackend{cfg: cfg}}
}

func (b *codebuddyBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	// The SDK bridge path in claudeBackend dials Anthropic directly via
	// claude-agent-sdk-go. cbc is a Tencent gateway routing to non-Anthropic
	// models; running the SDK bridge here would silently hit the wrong
	// endpoint with the wrong credentials. Force the stream-json subprocess
	// path regardless of what runtime_config said.
	opts.ClaudeUseSDKBridge = false
	return b.inner.Execute(ctx, prompt, opts)
}
