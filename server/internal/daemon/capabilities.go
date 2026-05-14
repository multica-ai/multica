// CEREBRO-PATCH(capabilities): persona integration changes.
package daemon

// providerCapabilities returns the static capability snapshot the daemon
// reports for a given provider type at register time. Persona's UI surfaces
// this alongside the abstract sandbox so an operator can see "what tools
// my runtime actually supports" without having to read provider docs.
//
// MVP scope: built-in tool list per provider. mcp_servers is empty because
// MCP config is per-agent in this codebase, not daemon-global; the runtime
// row therefore can't report a meaningful set. Once daemon-wide MCP lands,
// this is the function that grows the field.
//
// Conservative defaults: an unknown provider reports an empty payload
// rather than fabricated tool names, so persona's UI shows "(unknown)"
// instead of misleading data.
func providerCapabilities(providerType string) map[string]any {
	switch providerType {
	case "claude":
		return map[string]any{
			"providers": []string{"claude"},
			"tools": []string{
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
			"mcp_servers": []string{},
		}
	case "codex":
		// Codex has its own surface; report what we can and leave the rest
		// to a future provider expansion. Listed providers stay accurate so
		// downstream filters ("claude only" / "codex only") work today.
		return map[string]any{
			"providers":   []string{"codex"},
			"tools":       []string{},
			"mcp_servers": []string{},
		}
	default:
		return map[string]any{
			"providers":   []string{providerType},
			"tools":       []string{},
			"mcp_servers": []string{},
		}
	}
}
