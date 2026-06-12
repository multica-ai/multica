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
//   - Minimum interval: 15 minutes between wakeup creations for the same agent+issue.
//   - fire_at must be at least 15 minutes from now for trigger_type=time.
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
	add("- `fire_at` skal være mindst 15 minutter fra nu.\n")
	add("- Der må kun oprettes én pending wakeup pr. agent+issue ad gangen (inden for 15 minutter).\n")
	add("- Hvis agenten afviser wakeup-oprettelse, tjek om en eksisterende wakeup er tilstrækkelig.\n\n")
	return string(b)
}
