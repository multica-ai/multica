import type { AgentStatus } from "@multica/core/types";
import {
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
} from "lucide-react";

// Status visuals (color + dot). Labels are localized — read them from
// `useAgentsT().status[agent.status]` at the call site.
export const statusConfig: Record<AgentStatus, { color: string; dot: string }> = {
  idle: { color: "text-muted-foreground", dot: "bg-muted-foreground" },
  working: { color: "text-success", dot: "bg-success" },
  blocked: { color: "text-warning", dot: "bg-warning" },
  error: { color: "text-destructive", dot: "bg-destructive" },
  offline: { color: "text-muted-foreground/50", dot: "bg-muted-foreground/40" },
};

export type AgentTaskStatusKey =
  | "queued"
  | "dispatched"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";

// Task status visuals. Labels are localized via
// `useAgentsT().tasks.statusLabels[key]` at the call site.
export const taskStatusConfig: Record<
  AgentTaskStatusKey,
  { icon: typeof CheckCircle2; color: string }
> = {
  queued: { icon: Clock, color: "text-muted-foreground" },
  dispatched: { icon: Play, color: "text-info" },
  running: { icon: Loader2, color: "text-success" },
  completed: { icon: CheckCircle2, color: "text-success" },
  failed: { icon: XCircle, color: "text-destructive" },
  cancelled: { icon: XCircle, color: "text-muted-foreground" },
};
