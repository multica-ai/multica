import type { DiagnosticsPayload } from "./daemon-diagnostics";
import type { PublicHealthPayload } from "./daemon-health";

export type AutomaticRestartDecision = "restart" | "defer";

export function decideAutomaticRestart(
  health: PublicHealthPayload | null,
  diagnostics: DiagnosticsPayload | null,
): AutomaticRestartDecision {
  if (!health) return "defer";
  if (health.status !== "running") return "defer";
  if (!diagnostics || diagnostics.status !== "running") return "defer";
  return diagnostics.active_task_count === 0 ? "restart" : "defer";
}
