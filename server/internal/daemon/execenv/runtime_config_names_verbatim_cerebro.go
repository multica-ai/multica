package execenv

// CEREBRO-PATCH(runtime-config-names-verbatim): FIR-3986 — standing runtime-brief
// rule that stops agents from inventing translated names for things that already
// have one (issue → "sag", run → "kørsel", handoff → "overlevering").
//
// Why a new rule when four already existed: the prohibition was present in agent
// instructions, in the requesting user's profile, and in the plain-dansk-gate
// skill, and an agent still broke it across an entire issue thread. Every one of
// those wordings asks the writer to CLASSIFY each word mid-sentence ("is this a
// name that has a real name?"), and that judgement loses to fluency when the
// surrounding prose is Danish. This wording removes the judgement and replaces it
// with a single mechanical test that runs once, on the finished text.
//
// The last line is load-bearing: plain-dansk-gate and caveman both pull toward
// plain Danish, and with no stated precedence the product names lost. Naming the
// winner is what makes the rule survive contact with those skills.
//
// Emitted for EVERY task, not only issue-context ones — an agent writing in a
// chat session, a DM or a channel can invent a name just as easily.

// cerebroNamesVerbatimRule returns the standing "never translate a name" section
// for the agent runtime brief.
func cerebroNamesVerbatimRule() string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add("## Names\n\n")
	add("**Never translate a name.** Product, system, UI, status and command names are copied exactly, in any language: issue, run, handoff, wakeup, Workpad, eval, branch, draft, done.\n\n")
	add("Before sending: every noun that names a thing must be findable in the product's search. Not findable = you invented it.\n\n")
	add("Explain a name, never rename it. This beats every \"write plainly\" rule.\n\n")
	return string(b)
}
