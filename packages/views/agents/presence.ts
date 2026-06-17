// CEREBRO-PATCH(agent-availability-paused-visual): adds PauseCircle icon
// and a "paused" entry to availabilityConfig/availabilityOrder so the
// cerebro pause state surfaces on agent presence dots and filters.
import {
  AlertCircle,
  Archive,
  CircleDot,
  CircleSlash,
  Clock,
  Loader2,
  PauseCircle,
  PlugZap,
  type LucideIcon,
} from "lucide-react";
import type { AgentAvailability, Workload } from "@multica/core/agents";

// Visual mapping for the two presence dimensions, kept in matching shape
// so consumers can pick which to render. The two are independent — the
// dot reads only from availabilityConfig, the workload chip reads only
// from workloadConfig.
//
// Color tokens map to project semantic tokens (no hardcoded Tailwind colors):
//
//   AVAILABILITY (drives the dot everywhere a dot appears). Every dot is a
//   Slack-style presence indicator: a filled centre on a thin grey outline
//   ring (`border border-muted-foreground`). State reads from the centre fill,
//   not an alarming colour:
//     online    → success center + grey ring   (green)
//     unstable  → warning center + grey ring   (amber) — transient
//     paused    → background center + grey ring (empty/hollow) — not online
//     offline   → background center + grey ring (empty/hollow)
//     archived  → background center + grey ring (empty/hollow)
//
//   WORKLOAD (drives the optional workload chip on focused surfaces):
//     working   → brand           (blue)  has activity
//     queued    → warning         (amber) anomaly: nothing running but tasks
//                                          waiting (typically stuck on offline
//                                          runtime; brief flash on online is
//                                          a harmless race)
//     idle      → muted           (gray)  nothing on the plate
//
// `failed` / `completed` / `cancelled` deliberately have no top-level visual
// — those are historical context, surfaced via Recent Work + Inbox, not
// list-level summary state.

export interface AvailabilityVisual {
  label: string;
  // Background fill for the dot indicator.
  dotClass: string;
  // Foreground colour for the label text alongside the dot.
  textClass: string;
  // Icon used in larger badge contexts (detail header, hover card).
  icon: LucideIcon;
}

export const availabilityConfig: Record<AgentAvailability, AvailabilityVisual> = {
  online: {
    label: "Online",
    // CEREBRO-PATCH(status-slack-dot): grey outline ring on every dot (Slack look).
    // TECH-3686 follow-up (Jesper): online dot uses the standard, saturated
    // Multica green token (--success) so it reads clearly — not the washed-out
    // lighter green we tried earlier. No inset white ring inside the dot; the
    // only white is the avatar's ring-background separation, which reads as a
    // background gap (not a second white circle), per Jesper.
    dotClass:
      "bg-success border border-muted-foreground",
    textClass: "text-success",
    icon: CircleDot,
  },
  // CEREBRO-PATCH(status-slack-dot): inactive states (paused/offline/archived)
  // render as a hollow grey-ringed dot (Slack away style) — empty centre on a
  // grey outline, no red. State reads from fill (green vs empty), distinct by
  // shape. Supersedes the earlier red status-red-green treatment (TECH-3686).
  paused: {
    label: "Paused",
    dotClass: "bg-background border border-muted-foreground",
    textClass: "text-muted-foreground",
    icon: PauseCircle,
  },
  unstable: {
    label: "Unstable",
    dotClass: "bg-warning border border-muted-foreground",
    textClass: "text-warning",
    icon: PlugZap,
  },
  offline: {
    label: "Offline",
    dotClass: "bg-background border border-muted-foreground",
    textClass: "text-muted-foreground",
    icon: CircleSlash,
  },
  // Lifecycle state, not a runtime state — a retired agent. Hollow like
  // offline (it can't take work) but labelled distinctly so the user reads
  // "this agent is archived", not "temporarily unreachable".
  archived: {
    label: "Archived",
    dotClass: "bg-background border border-muted-foreground",
    textClass: "text-muted-foreground",
    icon: Archive,
  },
};

// Order used by availability filter chips so colours read in a natural
// progression rather than alphabetical.
export const availabilityOrder: AgentAvailability[] = [
  "online",
  "paused",
  "unstable",
  "offline",
];

export interface WorkloadVisual {
  label: string;
  // Foreground colour for icon + label text.
  textClass: string;
  // Icon used inline.
  icon: LucideIcon;
}

export const workloadConfig: Record<Workload, WorkloadVisual> = {
  working: {
    label: "Working",
    textClass: "text-brand",
    icon: Loader2,
  },
  queued: {
    // Amber chip: nothing running but tasks waiting. On an offline runtime
    // this is the "stuck" signal we explicitly surface (replacing the old
    // misleading "Running 0/N +Mq" copy).
    label: "Queued",
    textClass: "text-warning",
    icon: Clock,
  },
  idle: {
    label: "Idle",
    textClass: "text-muted-foreground",
    icon: AlertCircle,
  },
};

// Order used in any future workload chip group; actionable signals first.
export const workloadOrder: Workload[] = ["working", "queued", "idle"];
