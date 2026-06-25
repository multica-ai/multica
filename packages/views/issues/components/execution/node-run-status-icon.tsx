"use client";

import { AlertCircle, CheckCircle2, Circle, CircleOff, Clock, Loader2, MinusCircle, RotateCcw, UserCheck } from "lucide-react";
import type { NodeRunStatus } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

const STATUS_MAP: Record<NodeRunStatus, {
  icon: typeof Circle;
  className: string;
  spin?: boolean;
}> = {
  pending:             { icon: Circle,        className: "text-muted-foreground/40" },
  format_checking:     { icon: Loader2,       className: "text-blue-500", spin: true },
  format_ok:           { icon: CheckCircle2,  className: "text-amber-500" },
  format_failed:       { icon: AlertCircle,   className: "text-red-500" },
  worker_assigned:     { icon: UserCheck,     className: "text-amber-500" },
  working:             { icon: Loader2,       className: "text-blue-500", spin: true },
  awaiting_input:      { icon: Clock,         className: "text-amber-500" },
  awaiting_critic:     { icon: Clock,         className: "text-amber-500" },
  critic_reviewing:    { icon: Loader2,       className: "text-blue-500", spin: true },
  critic_approved:     { icon: CheckCircle2,  className: "text-green-500" },
  critic_rework:       { icon: RotateCcw,     className: "text-orange-500" },
  completed:           { icon: CheckCircle2,  className: "text-green-500" },
  failed:              { icon: AlertCircle,   className: "text-red-500" },
  blocked:             { icon: AlertCircle,   className: "text-red-500" },
  skipped:             { icon: MinusCircle,   className: "text-muted-foreground" },
  cancelled:           { icon: MinusCircle,   className: "text-muted-foreground" },
};

export interface NodeRunStatusIconProps {
  status: NodeRunStatus;
  className?: string;
}

export function NodeRunStatusIcon({ status, className }: NodeRunStatusIconProps) {
  const config = STATUS_MAP[status];

  if (!config) {
    return (
      <CircleOff
        data-testid="status-icon-fallback"
        className={cn("h-4 w-4 text-muted-foreground", className)}
      />
    );
  }

  const Icon = config.icon;
  return (
    <Icon
      data-testid={status === "pending" ? `status-icon-${status}` : "status-icon"}
      className={cn(
        "h-4 w-4 shrink-0",
        config.className,
        config.spin && "animate-spin",
        className,
      )}
    />
  );
}
