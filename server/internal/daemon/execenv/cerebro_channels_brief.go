package execenv

// cerebroChannelsBrief returns the Channels & Usage subsection injected into
// the "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
func cerebroChannelsBrief() string { // CEREBRO-PATCH(tech-3255-channels-brief): new sibling file — Channels & DMs and agent usage section for Available Commands (TECH-3255)
	return `### Channels & DMs

- ` + "`multica channel list [--kind channel|dm] [--all] [--output json]`" + ` — List channels and DMs in the workspace. ` + "`--kind channel`" + ` returns only group channels; ` + "`--kind dm`" + ` returns only direct-message threads. Omit ` + "`--kind`" + ` to list both. Add ` + "`--all`" + ` to include channels you are not a participant in.
- ` + "`multica channel messages <channel-id> [--since <RFC3339>] [--limit N] [--output json]`" + ` — List messages in a channel or DM. ` + "`--since`" + ` filters to messages newer than the given timestamp. ` + "`--limit`" + ` caps the number returned (default 100). The channel ID comes from ` + "`multica channel list`" + `.

### Agent Usage

- ` + "`multica agent usage [agent-id] [--days N] [--output json]`" + ` — Token and cost breakdown per agent for the past N days (default 30, max 365). Omit the agent ID to see all agents in the workspace. Columns: AGENT_ID, MODEL, INPUT_TOKENS, OUTPUT_TOKENS, CACHE_READ, CACHE_WRITE, COST_CENTS, TASKS.

`
}
