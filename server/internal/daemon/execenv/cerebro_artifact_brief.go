package execenv

// cerebroArtifactBrief returns the Documents & Artifacts subsection injected
// into the "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
func cerebroArtifactBrief() string { // CEREBRO-PATCH(cerebro-artifact-brief): new sibling file — Documents & Artifacts section for Available Commands (TECH-2949)
	// CEREBRO-PATCH(cerebro-artifact-brief-white-card): FIR-1905 — steer agents to the white-card mention link (not grey --attachment) for .md output across comment/chat/DM/channel. FIR-2577 — deduplicated: each rule stated once.
	return `### Documents & Artifacts

- ` + "`multica artifact create --kind <report|plan|decision|diagram|note> --title \"...\" [--body-stdin] [--issue <id>] [--folder <id>] [--origin-issue <id>]`" + ` — Create a typed document. Use for any output with a title: reports, analyses, plans, decision logs, diagrams. Do NOT paste long outputs in comments — create an artifact instead.
- ` + "`multica artifact folder list --output json`" + ` — List folders with their IDs (required before placing an artifact in a folder).
- ` + "`multica artifact folder create --name \"...\" [--parent <id>]`" + ` — Create a folder. Standard folders: Analyser, Planer, Beslutninger, Rapporter, Diagrammer.
- ` + "`multica document create --title \"...\" [--content-stdin] [--folder <id>]`" + ` — Shorthand for a markdown note artifact.

**When to create an artifact vs. a comment:** Comments are for status, short answers (< ~100 words), and questions back to the user. Artifacts are for everything with a title that the user will want to find again. When in doubt, create an artifact and reference it in the comment.

**Showing a document in a message (issue comment, chat, DM, channel):** pass ` + "`--artifact <id>`" + ` where the command supports it (` + "`issue comment add`" + `, ` + "`chat session send`" + `) — the platform renders the document as a compact **white card** that opens the full note editor. Where there is no flag (DM, channel), write a real markdown link ` + "`[document](mention://artifact/<id>)`" + ` in the message body instead. The bare string ` + "`mention://artifact/<id>`" + ` is NOT autolinked — it renders as plain text with no card.

**` + "`--attachment`" + ` is ONLY for raw binary files** — screenshots, image proof, PDFs, exported binaries. It renders as a grey raw-file row: not browsable, not editable, easy to miss. Never pass a ` + "`.md`" + ` you authored to ` + "`--attachment`" + ` — create it as an artifact and show the white card instead.

**Scope:** use ` + "`--issue <id>`" + ` when the artifact belongs to the current issue; omit for workspace-level output and add ` + "`--origin-issue <id>`" + ` to preserve the trail.

`
}
