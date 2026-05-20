// Package capabilities is the canonical source of truth for what a runtime
// provider can do. It replaces the daemon's previous one-line `switch` over
// provider names with a richer, per-provider declaration that Multica's
// permission layer can gate against.
//
// The package is intentionally pure — no I/O, no goroutines, no clock — so
// every caller (daemon register flow, persona attribute push, future
// drift-alarm) gets the same answer for the same provider, every time. Live
// discovery (reading the host's actual CLI binaries, scanning a workspace's
// MCP config) is layered on top via the Enrich functions, not baked into
// the static registry.
package capabilities

import "sort"

// Set is the capability snapshot we publish for a single runtime provider.
//
// The shape is deliberately flat and tag-driven so Persona's UI, the
// /api/runtimes/register payload, and any future capability-explorer all
// serialise to byte-identical JSON. New surfaces (e.g. webhook providers)
// can be appended without breaking existing consumers — the existing fields
// are stable.
type Set struct {
	// Providers is the canonical provider-name list this Set applies to. Most
	// of the time it's a single entry equal to the lookup key; the slice form
	// keeps room for aliases (e.g. "claude-code" → "claude") without forcing
	// the call site to know about them.
	Providers []string `json:"providers"`

	// Tools is the runtime's built-in tool surface — names a Multica admin
	// can grant or deny without inspecting the agent's MCP config. For Claude
	// Code that's Bash/Edit/Grep/…; for Cursor it's edit_file/read_file/…;
	// for runtimes without a static tool surface (firtal-gateway, gemini-cli
	// in tool-pass-through mode) the slice is empty and the MCPServers /
	// CLIBinaries fields carry the real surface instead.
	Tools []string `json:"tools"`

	// MCPServers is the list of MCP server names the runtime exposes by
	// default. Per-agent mcp_config overrides this at spawn time; the static
	// list is the baseline a workspace-default policy can pin to.
	MCPServers []string `json:"mcp_servers"`

	// Skills the runtime can load. Mostly relevant for Claude Code (which
	// uses ~/.claude/skills); other runtimes report nil and the field is
	// omitted from the wire payload via the AsMap helper below.
	Skills []string `json:"skills,omitempty"`

	// Subagents the runtime can spawn (e.g. Claude's "Task" tool sub-agents).
	// Used by the future subagent-cap permission type.
	Subagents []string `json:"subagents,omitempty"`

	// Hooks the runtime supports (PreToolUse, PostToolUse, OnStop, …). The
	// Cerebro Persona integration writes a PreToolUse hook today; the field
	// is here so the permission UI can gate which hooks an agent may load.
	Hooks []string `json:"hooks,omitempty"`

	// OutboundHosts is the static allowlist baseline for outbound HTTP from
	// inside the runtime — empty means "no static allowlist, defer to the
	// runtime's own network policy". Listed hosts are the runtime's defaults
	// (e.g. claude.ai for Claude Code), which Multica's network sandbox can
	// then narrow further per workspace.
	OutboundHosts []string `json:"outbound_hosts,omitempty"`

	// SecretBindings are the slot names the runtime expects to be populated
	// at spawn — e.g. ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY.
	// The grant API uses these so an admin can say "this agent may bind
	// ANTHROPIC_API_KEY but not OPENAI_API_KEY" without typing a free-form
	// string.
	SecretBindings []string `json:"secret_bindings,omitempty"`

	// CLIBinaries the runtime is known to invoke (gh, git, bq, kubectl, …).
	// Used by the binary-allowlist permission type. Static defaults live
	// here; live discovery (PATH walk on the daemon host) can enrich.
	CLIBinaries []string `json:"cli_binaries,omitempty"`

	// DiscoveryMethod records how this Set was produced. Today it's always
	// "static" for the built-in registry; the runtime-probe / agent-config
	// branches will set "probed" or "agent_config" respectively. Persona's
	// drift-alarm UI uses this to colour-code "is this a fresh measurement
	// or a stale default?".
	DiscoveryMethod string `json:"discovery_method,omitempty"`
}

// providerRegistry holds the static, hand-curated CapabilitySet for each
// runtime provider Cerebro knows about. Adding a new provider here is the
// single edit that propagates to: daemon register payload, persona actor
// attrs, future drift-alarm, and the Multica permission UI dropdowns.
//
// Sourcing rules:
//   - Tools, hooks, skills come from each runtime's public docs or its own
//     `--help` output. We only list capabilities we are 90%+ confident the
//     runtime exposes — fabricating entries here would surface bogus options
//     in the permission UI and erode operator trust.
//   - SecretBindings are the env-var names the runtime documents publicly.
//   - OutboundHosts and CLIBinaries stay empty when the runtime doesn't
//     publish a canonical list; the host-level discovery layer fills these
//     in by reading the actual binary's behaviour at register time.
var providerRegistry = map[string]Set{
	"claude": {
		Providers: []string{"claude"},
		Tools: []string{
			"Bash",
			"Edit",
			"Glob",
			"Grep",
			"MultiEdit",
			"NotebookEdit",
			"Read",
			"Task",
			"TodoWrite",
			"WebFetch",
			"WebSearch",
			"Write",
		},
		MCPServers:      []string{},
		Skills:          []string{},
		Subagents:       []string{"Task"},
		Hooks:           []string{"PreToolUse", "PostToolUse", "Stop", "SubagentStop", "Notification"},
		SecretBindings:  []string{"ANTHROPIC_API_KEY"},
		DiscoveryMethod: "static",
	},

	"codex": {
		Providers: []string{"codex"},
		Tools: []string{
			"apply_patch",
			"bash",
			"create_file",
			"str_replace_editor",
			"view",
		},
		MCPServers:      []string{},
		Subagents:       []string{},
		Hooks:           []string{"OnTaskStart", "OnTaskEnd"},
		SecretBindings:  []string{"OPENAI_API_KEY"},
		DiscoveryMethod: "static",
	},

	"cursor": {
		Providers: []string{"cursor"},
		Tools: []string{
			"codebase_search",
			"edit_file",
			"file_search",
			"grep_search",
			"list_dir",
			"read_file",
			"run_terminal_cmd",
			"web_search",
		},
		MCPServers:      []string{},
		Subagents:       []string{},
		Hooks:           []string{},
		SecretBindings:  []string{"CURSOR_API_KEY"},
		DiscoveryMethod: "static",
	},

	"gemini": {
		Providers: []string{"gemini"},
		Tools: []string{
			"read_file",
			"write_file",
			"replace_in_file",
			"shell",
			"web_fetch",
		},
		MCPServers:      []string{},
		Subagents:       []string{},
		Hooks:           []string{},
		SecretBindings:  []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"},
		DiscoveryMethod: "static",
	},

	"firtal-gateway": {
		Providers:      []string{"firtal-gateway"},
		Tools:          []string{},
		MCPServers:     []string{},
		Hooks:          []string{},
		SecretBindings: []string{"FIRTAL_GATEWAY_API_KEY"},
		// Firtal Gateway's surface is entirely MCP- and tool-pass-through;
		// real capabilities live on the agent's MCP config and are merged in
		// at spawn time via the Enrich path. The static row carries only the
		// secret binding so an admin can govern key-attachment.
		DiscoveryMethod: "static",
	},
}

// staticFallback is what we return for any provider name we haven't curated
// yet. It keeps the wire shape stable (every consumer sees the same keys)
// and tells Persona's drift-alarm UI that this runtime is unmapped, so the
// operator notices that we should add an entry to providerRegistry instead
// of silently shipping an empty allowlist.
func staticFallback(provider string) Set {
	return Set{
		Providers:       []string{provider},
		Tools:           []string{},
		MCPServers:      []string{},
		DiscoveryMethod: "unmapped",
	}
}

// For returns the static CapabilitySet for a provider. Unknown providers
// receive a stable empty-but-typed Set with DiscoveryMethod="unmapped" so
// downstream JSON consumers never have to handle missing keys.
//
// The returned Set is a *copy* — callers may sort/extend the slices without
// mutating the registry.
func For(provider string) Set {
	src, ok := providerRegistry[provider]
	if !ok {
		return staticFallback(provider)
	}
	return cloneSet(src)
}

// EnrichWithAgentMCPServers replaces the static MCP list with the per-agent
// configured servers. We do not merge — when an agent has its own mcp_config
// it is the authoritative list for that spawn, and the static defaults would
// only confuse the permission picture.
//
// names should already be sorted by the caller (the daemon does this when
// extracting mcp_config keys); we sort again here defensively so the
// resulting Set serialises stably regardless of input order.
func EnrichWithAgentMCPServers(s Set, names []string) Set {
	if len(names) == 0 {
		return s
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	s.MCPServers = out
	if s.DiscoveryMethod == "static" {
		s.DiscoveryMethod = "agent_config"
	}
	return s
}

// AsMap converts a Set to the legacy map[string]any shape the daemon's
// register payload and persona-attribute push have always used. Keeping
// this conversion in one place means the JSON contract is locked even as
// new fields appear on Set — callers continue to read caps["tools"] /
// caps["mcp_servers"] without needing to know about cli_binaries etc.
func AsMap(s Set) map[string]any {
	out := map[string]any{
		"providers":   nonNilStrings(s.Providers),
		"tools":       nonNilStrings(s.Tools),
		"mcp_servers": nonNilStrings(s.MCPServers),
	}
	if len(s.Skills) > 0 {
		out["skills"] = s.Skills
	}
	if len(s.Subagents) > 0 {
		out["subagents"] = s.Subagents
	}
	if len(s.Hooks) > 0 {
		out["hooks"] = s.Hooks
	}
	if len(s.OutboundHosts) > 0 {
		out["outbound_hosts"] = s.OutboundHosts
	}
	if len(s.SecretBindings) > 0 {
		out["secret_bindings"] = s.SecretBindings
	}
	if len(s.CLIBinaries) > 0 {
		out["cli_binaries"] = s.CLIBinaries
	}
	if s.DiscoveryMethod != "" {
		out["discovery_method"] = s.DiscoveryMethod
	}
	return out
}

// nonNilStrings turns a nil slice into an empty slice so JSON marshalling
// produces `[]` rather than `null` — Persona's UI iterates over these fields
// and a `null` value would crash the render. The daemon's earlier static
// switch made the same choice; we preserve it for byte-compatibility.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// cloneSet returns a deep-enough copy that callers can mutate slices without
// affecting the registry. The Set itself is small, so even a wasteful copy
// is cheaper than tracking ownership across the daemon / persona / register
// call sites.
func cloneSet(s Set) Set {
	return Set{
		Providers:       cloneStrings(s.Providers),
		Tools:           cloneStrings(s.Tools),
		MCPServers:      cloneStrings(s.MCPServers),
		Skills:          cloneStrings(s.Skills),
		Subagents:       cloneStrings(s.Subagents),
		Hooks:           cloneStrings(s.Hooks),
		OutboundHosts:   cloneStrings(s.OutboundHosts),
		SecretBindings:  cloneStrings(s.SecretBindings),
		CLIBinaries:     cloneStrings(s.CLIBinaries),
		DiscoveryMethod: s.DiscoveryMethod,
	}
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// KnownProviders lists every provider name we have a curated Set for. It
// exists so a future drift-alarm can compare the daemon's actually-running
// providers against the curated registry and flag the unmapped ones. Order
// is stable (alphabetic) so test fixtures don't churn.
func KnownProviders() []string {
	out := make([]string, 0, len(providerRegistry))
	for name := range providerRegistry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
