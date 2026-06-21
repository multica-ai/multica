package execenv

// CEREBRO-PATCH(runtime-config-claudemd-scope): FIR-1585 — standing runtime-brief
// rule that stops agents writing non-repo content into a repository's CLAUDE.md /
// AGENTS.md. The cost-optimization System-prompt-inspector showed a repository
// CLAUDE.md is injected verbatim into EVERY agent's prompt for that repo, so
// workspace/agent/process noise written there is paid for on every future run and
// drifts the file away from being a repo contract. Kept in its own cerebro-prefixed
// sibling file so the upstream file (runtime_config.go) needs only one marked call.

// cerebroClaudeMdScopeRule returns the standing "repository CLAUDE.md is repo-only"
// section for the agent runtime brief. Emitted for every task with an issue
// context, i.e. exactly where agents edit repository files.
func cerebroClaudeMdScopeRule() string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add("## Repository CLAUDE.md / AGENTS.md — repo-only, no exceptions\n\n")
	add("A repository's `CLAUDE.md` and `AGENTS.md` are injected into the prompt of EVERY agent that works in that repo, so every line is paid for on every future run. Keep them strictly about THIS codebase: architecture, build/test/lint commands, code conventions, and repo-specific gotchas.\n\n")
	add("**Never write into a repository's `CLAUDE.md` / `AGENTS.md`:**\n")
	add("- Workspace, company, or process context — anything not specific to this codebase.\n")
	add("- Agent identity, persona, or role instructions.\n")
	add("- Issue/task status, decisions, plans, or one-off notes — those go in comments, issue metadata, or artifacts.\n")
	add("- Cross-cutting platform or runtime rules that are not about this repository.\n\n")
	add("Those belong in workspace settings, agent instructions, comments, or artifacts — not the repo. Non-repo content in a repository instruction file bloats every future agent's prompt and is a no-go.\n\n")
	return string(b)
}
