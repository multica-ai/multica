// Derived "health" type for runtimes — the user-facing state we display
// in lists, cards, and tooltips. The raw server field is binary (online /
// offline + last_seen_at); this enum splits the offline state into three
// time-bucketed flavors so users can tell "just lost" from "long gone".

export type RuntimeHealth =
  | "online" // green — within heartbeat threshold
  | "recently_lost" // amber — offline < 5 minutes (likely transient)
  | "offline" // grey — offline 5 minutes ~ 6 days
  | "long_offline"; // destructive — offline beyond 6 days

// The health a runtime is *displayed* as. "unbound" is a display-only state:
// the runtime is reachable (raw status "online") but no agent is bound to it,
// so it cannot receive a dispatch. Reporting it as "online" reads as "ready to
// run" and has sent tasks into the void (SHAR-188/C6).
export type RuntimeDisplayHealth = RuntimeHealth | "unbound";
