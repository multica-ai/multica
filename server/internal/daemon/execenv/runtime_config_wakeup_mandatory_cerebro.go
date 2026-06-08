package execenv

// CEREBRO-PATCH(runtime-config-wakeup-mandatory): TECH-3121 — standing
// runtime-brief rule that forces agents to schedule a concrete wakeup whenever
// they comment about waiting, returning, or checking back. The incident pattern
// was: agents would write "I'll check back when X is done" without calling
// schedule_wakeup, then stop — leaving the issue silently stalled because no
// future run was ever queued. The "Long-running work — never busy-wait" rule
// already stops poll-loops; this companion rule closes the opposite gap: agents
// that exit without scheduling a wakeup and just *promise* to return.
// Emitted into the standing brief for tasks that operate on a real issue
// (assignment- and comment-triggered), where "wait and return" patterns arise.

// cerebroWakeupMandatoryRule returns the standing "set a wakeup when you wait"
// section for the agent runtime brief.
func cerebroWakeupMandatoryRule() string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add("## Wakeup before waiting — mandatory\n\n")
	add("If you comment that you will **check back**, **return**, **wait**, or **follow up** on anything, you MUST:\n\n")
	add("1. Call `schedule_wakeup` (MCP tool) **or** `multica wakeup create` **before** posting the comment.\n")
	add("2. Include the exact wakeup time in the comment itself — e.g. *\"wakeup set for 2026-06-09T10:00:00Z\"*.\n\n")
	add("**\"I'll check back later\" without a scheduled wakeup is not acceptable.** Set the wakeup first, then write the concrete RFC3339 timestamp in your comment. Vague promises without a scheduled return do not count — agents have no memory between runs and will never return unless a wakeup is explicitly set.\n\n")
	add("Use `trigger_type=time` + `fire_at=<RFC3339>` for a one-shot wakeup, or `trigger_type=recurring` + `interval_seconds=<seconds>` for repeated checks. Prefer MCP (`schedule_wakeup`) when available; fall back to `multica wakeup create --at <RFC3339>` if not.\n\n")
	return string(b)
}
