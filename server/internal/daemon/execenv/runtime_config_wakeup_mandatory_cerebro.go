package execenv

// CEREBRO-PATCH(runtime-config-wakeup-mandatory): TECH-3038 Phase 1 — standing
// runtime-brief section that documents the schedule_wakeup / list_wakeups /
// cancel_wakeup tools for agents. Does NOT mention trigger_type=recurring or
// interval_seconds: those are removed from the MCP schema in Phase 1 because
// automatic re-scheduling ("System Activity") is now owned by the platform, not
// individual agent runs. Agents use trigger_type=time for a single future
// wake-up; the platform re-schedules after each dispatch when needed.
//
// Key platform constraints baked in here so agents do not need to rediscover them:
//   - There is a small minimum gap between two time-wakeups for the same
//     agent+issue. It is a per-workspace setting (wakeup_min_interval_minutes,
//     default 5, hard floor 1), NOT a fixed platform-wide number — so this brief
//     deliberately does NOT hardcode a minute count. If fire_at is too soon the
//     API rejects it with the exact current minimum in the error text; the agent
//     reads that number and retries. This keeps the guidance correct even when
//     the setting changes, and stops stale numbers spreading (FIR-2544).
//   - Do NOT say "I'll check back" or "I'll wake up" without actually calling
//     schedule_wakeup — agents have no memory across runs.

// cerebroWakeupMandatoryRule returns the standing wakeup guidance section for
// the agent runtime brief. Emitted for every task with an issue context so
// agents know how to schedule future re-entry and understand the constraints.
func cerebroWakeupMandatoryRule() string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	// CEREBRO-PATCH(runtime-config-wakeup-mandatory): FIR-2577 — section rewritten in English; app-emitted text must never be Danish (workspace language rule), and one language keeps the brief unambiguous.
	add("## Wakeup before waiting — mandatory (ALL agents)\n\n")
	add("NEVER write \"I'll get back to this\", \"I'm waiting for X\", \"I'll check again later\" or similar — without, BEFORE the comment is posted:\n\n")
	add("1. **Scheduling a wakeup** via the `schedule_wakeup` MCP tool (or `multica wakeup create`).\n")
	add("2. **Writing the exact fire time in the comment** — e.g. \"wakeup set for 2026-06-09T10:00:00Z\".\n\n")
	add("**\"I'll be back soon\" WITHOUT a scheduled wakeup = not accepted.** Agents have no memory between runs — without a concrete wakeup the agent never returns and the issue stalls silently. Schedule the wakeup FIRST, then post, and include the time. A vague promise does not count.\n\n")
	add("Use `trigger_type=time` + `fire_at=<RFC3339>` for a single future wakeup. Prefer MCP (`schedule_wakeup`) when available; otherwise `multica wakeup create --at <RFC3339>`.\n\n")
	add("**Platform constraints (enforced server-side):**\n")
	// CEREBRO-PATCH(runtime-config-wakeup-mandatory): FIR-2544 — describe the wakeup minimum as a self-correcting per-workspace value, never a hardcoded minute count, so stale numbers can't spread.
	add("- There is a small minimum interval between two time-wakeups on the same agent+issue. The limit is a per-workspace setting (in **Cerebro features**) and is typically a few minutes — there is NO fixed platform-wide number to memorize.\n")
	add("- Do not learn a specific number: just set the `fire_at` you actually need. If it is too soon, the API rejects it with the exact current minimum in the error text (e.g. \"fire_at must be at least N minutes from now\") — use that number and retry.\n")
	add("- Only one pending wakeup per agent+issue at a time; create a new one after the previous one has fired or been cancelled.\n\n")
	return string(b)
}
