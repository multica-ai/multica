package execenv

// cerebroArtifactBrief returns the Documents & Artifacts subsection injected
// into the "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
func cerebroArtifactBrief() string { // CEREBRO-PATCH(cerebro-artifact-brief): new sibling file — Documents & Artifacts section for Available Commands (TECH-2949)
	return `### Documents & Artifacts

- ` + "`multica artifact create --kind <report|plan|decision|diagram|note> --title \"...\" [--body-stdin] [--issue <id>] [--folder <id>] [--origin-issue <id>]`" + ` — Create a typed document. Use for any output with a title: reports, analyses, plans, decision logs, diagrams. Do NOT paste long outputs in comments — create an artifact instead.
- ` + "`multica artifact folder list --output json`" + ` — List folders with their IDs (required before placing an artifact in a folder).
- ` + "`multica artifact folder create --name \"...\" [--parent <id>]`" + ` — Create a folder. Standard folders: Analyser, Planer, Beslutninger, Rapporter, Diagrammer.
- ` + "`multica document create --title \"...\" [--content-stdin] [--folder <id>]`" + ` — Shorthand for a markdown note artifact.

**When to create an artifact vs. a comment:** Comments are for status, short answers (< ~100 words), and questions back to the user. Artifacts are for everything with a title that the user will want to find again. When in doubt, create an artifact and reference it in the comment.

**Scope:** use ` + "`--issue <id>`" + ` when the artifact belongs to the current issue; omit for workspace-level output and add ` + "`--origin-issue <id>`" + ` to preserve the trail.

`
}
