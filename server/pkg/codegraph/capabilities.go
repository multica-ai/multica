package codegraph

// SupportsLocalTools reports the tool delivery path, not successful model use.
// Keep providers aligned with packages/core/agents/mcp-support.ts.
func SupportsLocalTools(mode, provider string) bool {
	if mode != "local" {
		return false
	}
	switch provider {
	case "claude", "codebuddy", "codearts", "codex", "cursor", "grok", "hermes", "kimi",
		"reasonix", "dsh", "kiro", "opencode", "openclaw", "qoder", "qoderclicn", "qwen",
		"qwenpaw", "mcode", "traecli", "dim", "omp":
		return true
	default:
		return false
	}
}
