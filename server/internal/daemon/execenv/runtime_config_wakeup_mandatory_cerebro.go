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
	add("## Wakeup ved ventetid — obligatorisk (gælder ALLE agenter)\n\n")
	add("Skriv ALDRIG \"jeg vender tilbage\", \"jeg afventer\", \"jeg checker igen\" eller lignende — uden at du FØR kommentaren sendes:\n\n")
	add("1. **Sætter en wakeup** via `schedule_wakeup` MCP-tool'et (eller `multica wakeup create`).\n")
	add("2. **Skriver det præcise tidspunkt i kommentaren** — f.eks. \"wakeup sat til 2026-06-09T10:00:00Z\".\n\n")
	add("**\"Jeg vender tilbage snart\" UDEN et sat wakeup = ikke godkendt.** Agenter har ingen hukommelse mellem kørsler — uden en konkret wakeup vender agenten aldrig tilbage, og issuet staller stille. Sæt wakeup'en FØR du poster, skriv tidspunktet. Et vagt løfte tæller ikke.\n\n")
	add("Brug `trigger_type=time` + `fire_at=<RFC3339>` for én enkelt fremtidig vækker. Foretruk MCP (`schedule_wakeup`) når tilgængeligt; ellers `multica wakeup create --at <RFC3339>`.\n\n")
	add("**Platform-begrænsninger (håndhæves server-side):**\n")
	// CEREBRO-PATCH(runtime-config-wakeup-mandatory): FIR-2544 — describe the wakeup minimum as a self-correcting per-workspace value, never a hardcoded minute count, so stale numbers can't spread.
	add("- Der er et lille minimums-interval mellem to time-wakeups på samme agent+issue. Grænsen sættes pr. workspace (i **Cerebro-features**) og er typisk få minutter — der er INGEN fast platform-grænse du skal huske udenad.\n")
	add("- Lær ikke et bestemt tal: sæt bare det `fire_at` du reelt har brug for. Er det for tidligt, afviser API'et med det præcise minimum i fejlteksten (fx \"fire_at must be at least N minutes from now\") — brug det tal og prøv igen.\n")
	add("- Der må kun være én pending wakeup pr. agent+issue ad gangen; opret en ny når den gamle er fyret eller aflyst.\n\n")
	return string(b)
}
