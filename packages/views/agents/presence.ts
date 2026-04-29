import {
  AlertCircle,
  CheckCircle2,
  CircleDot,
  CircleSlash,
  Loader2,
  PauseCircle,
  PlugZap,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import type { AgentAvailability, LastTaskState } from "@multica/core/agents";
import type { TaskFailureReason } from "@multica/core/types";

// Visual mapping for the two presence dimensions, kept in matching shape
// so consumers can pick which to render. The two are independent — the
// dot reads only from availabilityConfig, the last-task chip reads only
// from taskStateConfig.
//
// Color tokens map to project semantic tokens (no hardcoded Tailwind colors):
//
//   AVAILABILITY (drives the dot everywhere a dot appears):
//     online    → success         (green)
//     unstable  → warning         (amber) — pairs with the runtime card's amber
//     offline   → muted-foreground (gray)
//
//   LAST TASK STATE (drives the optional last-task chip on focused surfaces):
//     running   → brand           (blue)  has activity
//     completed → success         (green) all good
//     failed    → destructive     (red)
//     cancelled → muted           (gray)
//     idle      → muted           (gray)  no history
//
// Critically: `failed` colour appears ONLY in the last-task chip, never
// on the dot. A runtime-healthy agent whose last task failed shows a
// green dot + a red "Failed" chip — the dot stops being sticky-red.

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
    dotClass: "bg-success",
    textClass: "text-success",
    icon: CircleDot,
  },
  unstable: {
    label: "Unstable",
    dotClass: "bg-warning",
    textClass: "text-warning",
    icon: PlugZap,
  },
  offline: {
    label: "Offline",
    dotClass: "bg-muted-foreground/40",
    textClass: "text-muted-foreground",
    icon: CircleSlash,
  },
};

// Order used by availability filter chips so colours read in a natural
// progression rather than alphabetical.
export const availabilityOrder: AgentAvailability[] = [
  "online",
  "unstable",
  "offline",
];

export interface TaskStateVisual {
  label: string;
  // Foreground colour for icon + label text.
  textClass: string;
  // Icon used inline.
  icon: LucideIcon;
}

export const taskStateConfig: Record<LastTaskState, TaskStateVisual> = {
  running: {
    label: "Running",
    textClass: "text-brand",
    icon: Loader2,
  },
  completed: {
    label: "Completed",
    textClass: "text-success",
    icon: CheckCircle2,
  },
  failed: {
    label: "Failed",
    textClass: "text-destructive",
    icon: XCircle,
  },
  cancelled: {
    label: "Cancelled",
    textClass: "text-muted-foreground",
    icon: PauseCircle,
  },
  idle: {
    label: "Idle",
    textClass: "text-muted-foreground",
    icon: AlertCircle,
  },
};

// Order used by last-run filter chips. Actionable signals first
// (running / failed) before passive ones (idle / cancelled).
export const lastTaskOrder: LastTaskState[] = [
  "running",
  "failed",
  "completed",
  "cancelled",
  "idle",
];

// Human-readable copy for the back-end task failure reason enum. Surfaced
// in the hover card and detail header when lastTask === "failed".
export const failureReasonLabel: Record<TaskFailureReason, string> = {
  agent_error: "Agent execution error",
  timeout: "Task timed out",
  runtime_offline: "Daemon offline",
  runtime_recovery: "Daemon restarted",
  manual: "Cancelled by user",
};
