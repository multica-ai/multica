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
//
// ACPClient marks a provider whose before-call seam is the Agent Client
// Protocol itself: the agent asks its client for permission before every tool
// call, and the daemon IS that client. There is no settings file and no
// extension to install — the gate is in-process in the daemon, so it cannot be
// switched off from outside. See server/pkg/agent/cerebro_acp_tool_policy.go.
type ProviderAdapter struct {
	Provider           string
	HookEvent          string
	CompleteBeforeCall bool
	Harness            bool
	ACPClient          bool
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
	// Hermes, Kimi and Kiro are one ACP family driven by the same client
	// (server/pkg/agent/hermes.go). ACP requires the agent to ask its client
	// for permission before it runs a tool, so the daemon already sits on the
	// before-call seam for built-in and MCP tools alike — no provider-native
	// hook file and no installed extension. The gate answers only once-scoped
	// approvals, so every call is resolved rather than pre-approved for the
	// session, and it denies when no policy callback is wired.
	//
	// CompleteBeforeCall here rests on the ACP contract, not yet on a measured
	// run: the protocol defines the permission request as the thing an agent
	// sends before running a tool, and the auto-approve modes that suppressed
	// it (HERMES_YOLO_MODE, --trust-all-tools) are gone. What is NOT yet proven
	// on a Firtal host is that a given CLI build asks for EVERY tool rather
	// than only the mutating ones — the pre-existing note in hermes.go says
	// kimi asks "on every Shell / file-mutating tool call", which would leave
	// read-only tools ungated. Confirm with one live run per CLI (drive a
	// read-only tool with a Deny in place and assert it does not execute)
	// before treating this row as measured. If a build turns out to ask
	// selectively, that CLI must move back out of this map rather than keep a
	// contract it does not satisfy.
	// OpenCode has no provider-native hook file either, but it loads plugins
	// from .opencode/plugin and fires tool.execute.before ahead of every tool
	// it runs; throwing from that hook aborts the call. The Firtal plugin
	// (packages/cerebro-opencode-harness) is therefore its adapter, and
	// prepareOpenCodeHarness refuses the spawn when the plugin is not on disk.
	"opencode": {Provider: "opencode", HookEvent: "tool.execute.before", CompleteBeforeCall: true, Harness: true},
	"hermes":   {Provider: "hermes", HookEvent: "session/request_permission", CompleteBeforeCall: true, ACPClient: true},
	"kimi":   {Provider: "kimi", HookEvent: "session/request_permission", CompleteBeforeCall: true, ACPClient: true},
	"kiro":   {Provider: "kiro", HookEvent: "session/request_permission", CompleteBeforeCall: true, ACPClient: true},
}

// ProviderAdapterFor returns the enforcement contract for one local provider.
// An absent or incomplete contract is deliberately not runnable.
func ProviderAdapterFor(provider string) (ProviderAdapter, bool) {
	adapter, ok := providerAdapters[provider]
	return adapter, ok && adapter.CompleteBeforeCall
}
