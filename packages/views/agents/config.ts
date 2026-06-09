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

export type TaskQueueBucket = "blocked" | "queued" | "running" | "failed" | "completed" | "cancelled";
export type TaskReviewTone = "failed" | "long-running";
export type TaskReviewFlag = {
  tone: TaskReviewTone;
  label: string;
  detail: string;
};

export const LONG_RUNNING_TASK_MS = 30 * 60 * 1000;

export const taskStatusConfig: Record<string, { label: string; icon: LucideIcon; color: string }> = {
  queued: { label: "Queued", icon: Clock, color: "text-muted-foreground" },
  dispatched: { label: "Dispatched", icon: Play, color: "text-info" },
  running: { label: "Running", icon: Loader2, color: "text-success" },
  completed: { label: "Completed", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  cancelled: { label: "Cancelled", icon: XCircle, color: "text-muted-foreground" },
};

function formatElapsedForReview(ms: number): string {
  const totalMinutes = Math.floor(ms / 60_000);
  if (totalMinutes < 60) return `${totalMinutes}m`;

  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

function summarizeTaskError(error: string | null | undefined): string {
  const normalized = error?.replace(/\s+/g, " ").trim() ?? "";
  if (!normalized) return "Latest run failed and should be checked before retrying.";
  return normalized.length > 140 ? `${normalized.slice(0, 137)}...` : normalized;
}

export function getTaskQueueBucket(
  task: Pick<AgentTask, "status">,
  issue?: Pick<Issue, "blocked_by_count">,
): TaskQueueBucket {
  if (task.status === "queued" && (issue?.blocked_by_count ?? 0) > 0) return "blocked";
  if (task.status === "queued") return "queued";
  if (task.status === "dispatched" || task.status === "running") return "running";
  if (task.status === "failed") return "failed";
  if (task.status === "completed") return "completed";
  return "cancelled";
}

export function getTaskReviewFlag(
  task: Pick<AgentTask, "status" | "error" | "started_at" | "dispatched_at">,
  now = Date.now(),
): TaskReviewFlag | null {
  if (task.status === "failed") {
    return {
      tone: "failed",
      label: "Failed",
      detail: summarizeTaskError(task.error),
    };
  }

  if (task.status !== "running" && task.status !== "dispatched") return null;

  const activeSince = task.started_at ?? task.dispatched_at;
  if (!activeSince) return null;

  const elapsedMs = now - new Date(activeSince).getTime();
  if (!Number.isFinite(elapsedMs) || elapsedMs < LONG_RUNNING_TASK_MS) return null;

  return {
    tone: "long-running",
    label: "Long-running",
    detail: `Active for ${formatElapsedForReview(elapsedMs)} and may need a check-in.`,
  };
}

export function getTaskQueueDisplay(
  task: Pick<AgentTask, "status">,
  issue?: Pick<Issue, "blocked_by_count">,
): TaskQueueDisplay {
  const blockerCount = issue?.blocked_by_count ?? 0;
  const bucket = getTaskQueueBucket(task, issue);

  if (bucket === "blocked") {
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
      bucket === "running"
        ? (task.status === "dispatched" ? ("dispatched" as const) : ("running" as const))
        : ("default" as const),
    detail: null,
  };
}
