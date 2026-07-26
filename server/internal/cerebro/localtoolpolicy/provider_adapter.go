package localtoolpolicy

// ProviderAdapter is the data-driven contract a local runtime must satisfy
// before the daemon may start it. CompleteBeforeCall means every provider-local
// function tool, including direct MCP tools, reaches the configured hook before
// execution.
//
// Harness marks a provider that reaches the same before-call seam through a
// Firtal-owned runtime extension instead of a provider-native settings file.
// The extension is the enforcement mechanism, so a harness provider is only
// runnable while its harness is actually installed — the daemon must refuse to
// start it otherwise, exactly as it refuses a provider with no adapter at all.
type ProviderAdapter struct {
	Provider           string
	HookEvent          string
	CompleteBeforeCall bool
	Harness            bool
}

var providerAdapters = map[string]ProviderAdapter{
	"claude": {Provider: "claude", HookEvent: "PreToolUse", CompleteBeforeCall: true},
	"codex":  {Provider: "codex", HookEvent: "PreToolUse", CompleteBeforeCall: true},
	"cursor": {Provider: "cursor", HookEvent: "preToolUse", CompleteBeforeCall: true},
	"gemini": {Provider: "gemini", HookEvent: "BeforeTool", CompleteBeforeCall: true},
	// Pi has no provider-native hook file and no built-in MCP. The Firtal Pi
	// harness (packages/cerebro-pi-harness) registers every Multica tool into
	// Pi's own tool registry and gates the registry with pi.on("tool_call"),
	// which fires before execution for built-in and registered tools alike and
	// fails closed under the enforce stage — the same completeness Claude's
	// PreToolUse gives. See preparePiHarness for the enabled-or-refuse check.
	"pi": {Provider: "pi", HookEvent: "tool_call", CompleteBeforeCall: true, Harness: true},
}

// ProviderAdapterFor returns the enforcement contract for one local provider.
// An absent or incomplete contract is deliberately not runnable.
func ProviderAdapterFor(provider string) (ProviderAdapter, bool) {
	adapter, ok := providerAdapters[provider]
	return adapter, ok && adapter.CompleteBeforeCall
}
