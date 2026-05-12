// CEREBRO-PATCH(runtime-health-paused): cerebro modification of upstream file
// Derived "health" type for runtimes — the user-facing state we display
// in lists, cards, and tooltips. The raw server field is binary (online /
// offline + last_seen_at); this enum splits the offline state into three
// time-bucketed flavors so users can tell "just lost" from "long gone".
//
// "paused" is a cerebro addition: when the runtime is explicitly paused
// (manual or auto-pause via rate-limit / monthly-cap detection), pause
// takes precedence over heartbeat-derived state. The daemon may still be
// online; the user-visible truth is that work is gated by the pause.

export type RuntimeHealth =
  | "online" // green — within heartbeat threshold
  | "paused" // brand — orchestrationally paused (cerebro); daemon may still heartbeat
  | "recently_lost" // amber — offline < 5 minutes (likely transient)
  | "offline" // grey — offline 5 minutes ~ 7 days
  | "about_to_gc"; // dim — within 1 day of the 7-day GC threshold
