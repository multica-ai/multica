package localtoolpolicy

// ProviderAdapter is the data-driven contract a local runtime must satisfy
// before the daemon may start it. CompleteBeforeCall means every provider-local
// function tool, including direct MCP tools, reaches the configured hook before
// execution.
type ProviderAdapter struct {
	Provider           string
	HookEvent          string
	CompleteBeforeCall bool
}

var providerAdapters = map[string]ProviderAdapter{
	"claude": {Provider: "claude", HookEvent: "PreToolUse", CompleteBeforeCall: true},
	"codex":  {Provider: "codex", HookEvent: "PreToolUse", CompleteBeforeCall: true},
	"cursor": {Provider: "cursor", HookEvent: "preToolUse", CompleteBeforeCall: true},
	"gemini": {Provider: "gemini", HookEvent: "BeforeTool", CompleteBeforeCall: true},
}

// ProviderAdapterFor returns the enforcement contract for one local provider.
// An absent or incomplete contract is deliberately not runnable.
func ProviderAdapterFor(provider string) (ProviderAdapter, bool) {
	adapter, ok := providerAdapters[provider]
	return adapter, ok && adapter.CompleteBeforeCall
}
