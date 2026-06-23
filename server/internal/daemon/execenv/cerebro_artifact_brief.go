package execenv

// cerebroArtifactBrief returns the Documents & Artifacts subsection injected
// into the "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
func cerebroArtifactBrief() string { // CEREBRO-PATCH(cerebro-artifact-brief): new sibling file — Documents & Artifacts section for Available Commands (TECH-2949)
	// CEREBRO-PATCH(cerebro-artifact-brief-white-card): FIR-1905 — steer agents to the white-card mention link (not grey --attachment) for .md output across comment/chat/DM/channel
	return `### Documents & Artifacts

- ` + "`multica artifact create --kind <report|plan|decision|diagram|note> --title \"...\" [--body-stdin] [--issue <id>] [--folder <id>] [--origin-issue <id>]`" + ` — Create a typed document. Use for any output with a title: reports, analyses, plans, decision logs, diagrams. Do NOT paste long outputs in comments — create an artifact instead.
- ` + "`multica artifact folder list --output json`" + ` — List folders with their IDs (required before placing an artifact in a folder).
- ` + "`multica artifact folder create --name \"...\" [--parent <id>]`" + ` — Create a folder. Standard folders: Analyser, Planer, Beslutninger, Rapporter, Diagrammer.
- ` + "`multica document create --title \"...\" [--content-stdin] [--folder <id>]`" + ` — Shorthand for a markdown note artifact.

**When to create an artifact vs. a comment:** Comments are for status, short answers (< ~100 words), and questions back to the user. Artifacts are for everything with a title that the user will want to find again. When in doubt, create an artifact and reference it in the comment.

**Show a document on a comment / chat / DM / channel — use the white card, never a grey ` + "`--attachment`" + `.** When your titled output (` + "`.md`" + ` report, plan, analysis, note) is meant to appear in a message, create it with ` + "`multica artifact create`" + ` / ` + "`multica document create`" + ` and then put its ` + "`mention://artifact/<id>`" + ` link **inside the message body**. The platform renders that link as a compact **white card**; clicking it opens the full note editor. This is the rule on all four message surfaces:
- **Issue comment** — write the ` + "`mention://artifact/<id>`" + ` link in the ` + "`--content`" + ` body (HEREDOC over ` + "`--content-stdin`" + `, per ## Comment Formatting).
- **Chat session** (` + "`multica chat session send`" + `) — put the ` + "`mention://artifact/<id>`" + ` link in ` + "`--content`" + `.
- **DM** and **channel** — same: the artifact's ` + "`mention://artifact/<id>`" + ` link goes in the message text.

**` + "`--attachment`" + ` is ONLY for raw binary files** — screenshots, image proof, PDFs, exported binaries. It uploads the file as a **grey raw-file row**, which is the WRONG surface for an agent's own ` + "`.md`" + ` output: a grey row is not browsable, not editable, and easy to miss. Never pass a ` + "`.md`" + ` you authored to ` + "`--attachment`" + `; create it as an artifact and link the white card instead. Mental model: **grey row = uploaded raw file; white card = a real document/note.**

> A first-class ` + "`--artifact <id>`" + ` flag for ` + "`issue comment add`" + ` / ` + "`chat session send`" + ` (and channel/DM) is landing under FIR-1904 — it attaches the white card without hand-writing the mention link. Prefer it once it ships; until then the ` + "`mention://artifact/<id>`" + ` link in the body is the correct, shippable approach.

**Scope:** use ` + "`--issue <id>`" + ` when the artifact belongs to the current issue; omit for workspace-level output and add ` + "`--origin-issue <id>`" + ` to preserve the trail.

`
}
