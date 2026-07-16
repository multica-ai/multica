package execenv

// cerebroFeatureBrief returns the Cerebro Feature Flags subsection injected
// into the "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
//
// It documents the read-only `multica feature` CLI (see
// server/cmd/multica/cerebro_feature.go, FIR-3009) so agents can look up which
// cerebro features are actually active in this workspace instead of guessing
// (FIR-3377).
func cerebroFeatureBrief() string { // CEREBRO-PATCH(cerebro-feature-brief): FIR-3377 new sibling file — Cerebro Feature Flags section for Available Commands
	return `### Cerebro Feature Flags

Look up which cerebro features are active for you in this workspace instead of assuming. Read-only — these commands never change a flag.

- ` + "`multica feature list [--group <g>] [--state on|off] [--output json]`" + ` — list every cerebro feature with its effective state. Columns: KEY, STATE, SOURCE, GROUP, LABEL. Filter with ` + "`--state on`" + ` to see only what is active, or ` + "`--group <g>`" + ` (e.g. permissions, agents, issues) to narrow to one area.
- ` + "`multica feature get <key> [--output json]`" + ` — show one feature including its description and how its value resolves.

STATE is ` + "`on`" + ` / ` + "`off`" + ` (or ` + "`forced on`" + ` / ` + "`forced off`" + ` when a workspace admin has locked it). SOURCE tells you where the value came from: ` + "`default`" + ` (registry default), ` + "`personal`" + ` (your own override), ` + "`workspace`" + `, or ` + "`workspace (locked)`" + `. A locked workspace value wins over your personal override.

`
}
