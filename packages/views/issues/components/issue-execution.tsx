"use client";

import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  Loader2,
} from "lucide-react";
import type { IssueExecutionSummary } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { timeAgo } from "@multica/core/utils";

type ExecutionPresentation = {
  label: string;
  detail?: string;
  className: string;
  icon: typeof Clock3;
  animate?: boolean;
};

function getExecutionPresentation(
  summary: IssueExecutionSummary | undefined,
): ExecutionPresentation | null {
  if (!summary || summary.state === "idle") return null;

  switch (summary.state) {
    case "running":
      return {
        label: "Running",
        detail:
          summary.running_count > 1
            ? `${summary.running_count} active`
            : undefined,
        className: "border-success/30 bg-success/10 text-success",
        icon: Loader2,
        animate: true,
      };
    case "queued":
      return {
        label: "Queued",
        detail:
          summary.queued_count > 1
            ? `${summary.queued_count} queued`
            : undefined,
        className: "border-muted-foreground/20 bg-muted text-muted-foreground",
        icon: Clock3,
      };
    case "failed":
      return {
        label: "Failed",
        detail: summary.latest_completed_at
          ? timeAgo(summary.latest_completed_at)
          : undefined,
        className: "border-destructive/30 bg-destructive/10 text-destructive",
        icon: AlertCircle,
      };
    case "completed":
      return {
        label: "Completed",
        detail: summary.latest_completed_at
          ? timeAgo(summary.latest_completed_at)
          : undefined,
        className: "border-success/20 bg-success/10 text-success",
        icon: CheckCircle2,
      };
    default:
      return null;
  }
}

export function IssueExecutionBadge({
  summary,
  className,
}: {
  summary: IssueExecutionSummary | undefined;
  className?: string;
}) {
  const presentation = getExecutionPresentation(summary);
  if (!presentation) return null;

  const Icon = presentation.icon;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[11px] font-medium",
        presentation.className,
        className,
      )}
    >
      <Icon
        className={cn("h-3 w-3", presentation.animate && "animate-spin")}
      />
      <span>{presentation.label}</span>
      {presentation.detail && (
        <span className="opacity-80">{presentation.detail}</span>
      )}
    </span>
  );
}

export function IssueExecutionBanner({
  summary,
  className,
}: {
  summary: IssueExecutionSummary | undefined;
  className?: string;
}) {
  const presentation = getExecutionPresentation(summary);
  if (!presentation) return null;

  const Icon = presentation.icon;

  return (
    <div
      className={cn(
        "rounded-lg border px-3 py-2",
        presentation.className,
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <Icon
          className={cn("h-4 w-4 shrink-0", presentation.animate && "animate-spin")}
        />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{presentation.label}</div>
          {presentation.detail && (
            <div className="text-xs opacity-80">{presentation.detail}</div>
          )}
        </div>
      </div>
      {(summary?.latest_trigger_excerpt || summary?.latest_error) && (
        <p className="mt-2 line-clamp-2 text-xs opacity-90">
          {summary.latest_error ?? summary.latest_trigger_excerpt}
        </p>
      )}
    </div>
  );
}
