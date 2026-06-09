import type { AgentStatus, AgentTask, Issue } from "@multica/core/types";
import {
  AlertCircle,
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
  type LucideIcon,
} from "lucide-react";

export const statusConfig: Record<AgentStatus, { label: string; color: string; dot: string }> = {
  idle: { label: "Idle", color: "text-muted-foreground", dot: "bg-muted-foreground" },
  working: { label: "Working", color: "text-success", dot: "bg-success" },
  blocked: { label: "Blocked", color: "text-warning", dot: "bg-warning" },
  error: { label: "Error", color: "text-destructive", dot: "bg-destructive" },
  offline: { label: "Offline", color: "text-muted-foreground/50", dot: "bg-muted-foreground/40" },
};

export type TaskQueueDisplay = {
  label: string;
  icon: LucideIcon;
  color: string;
  tone: "default" | "dispatched" | "running" | "blocked";
  detail: string | null;
};

export const taskStatusConfig: Record<string, { label: string; icon: LucideIcon; color: string }> = {
  queued: { label: "Queued", icon: Clock, color: "text-muted-foreground" },
  dispatched: { label: "Dispatched", icon: Play, color: "text-info" },
  running: { label: "Running", icon: Loader2, color: "text-success" },
  completed: { label: "Completed", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  cancelled: { label: "Cancelled", icon: XCircle, color: "text-muted-foreground" },
};

export function getTaskQueueDisplay(
  task: Pick<AgentTask, "status">,
  issue?: Pick<Issue, "blocked_by_count">,
): TaskQueueDisplay {
  const blockerCount = issue?.blocked_by_count ?? 0;

  if (task.status === "queued" && blockerCount > 0) {
    return {
      label: "Blocked",
      icon: AlertCircle,
      color: "text-warning",
      tone: "blocked" as const,
      detail:
        blockerCount === 1
          ? "Waiting on 1 unresolved dependency"
          : `Waiting on ${blockerCount} unresolved dependencies`,
    };
  }

  const config = taskStatusConfig[task.status] ?? taskStatusConfig.queued ?? { label: "Queued", icon: Clock, color: "text-muted-foreground" };
  return {
    label: config.label,
    icon: config.icon,
    color: config.color,
    tone:
      task.status === "running"
        ? ("running" as const)
        : task.status === "dispatched"
          ? ("dispatched" as const)
          : ("default" as const),
    detail: null,
  };
}
