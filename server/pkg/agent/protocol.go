package agent

// ProtocolFamily maps a provider key to its base protocol identity.
//
// Variant providers (codebuddy, claude-internal, codex-internal,
// gemini-internal) speak the same wire/CLI protocol as their parent
// (Claude Code / Codex / Gemini) and reuse the parent's backend
// implementation verbatim. Every per-provider behaviour switch in the
// daemon — CLAUDE.md vs AGENTS.md vs GEMINI.md selection, the
// ~/.claude/skills home, CODEX_HOME setup, ClaudeArgs/CodexArgs
// routing, comment reply templates — should be keyed off this value,
// not the raw provider, otherwise variants silently fall through to
// the default branch and lose feature parity with the original.
//
// Returns the input unchanged for unknown / non-aliased providers so
// existing call sites keep their current behaviour for the 11 base
// providers.
func ProtocolFamily(provider string) string {
	switch provider {
	case "codebuddy", "claude-internal":
		return "claude"
	case "codex-internal":
		return "codex"
	case "gemini-internal":
		return "gemini"
	default:
		return provider
	}
}
