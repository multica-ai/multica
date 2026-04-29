"use client";

import { useEffect } from "react";
import { Bot, Clock3, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Issue } from "@/shared/types";
import { useIssueTaskStore } from "@/features/issues/stores/issue-task-store";

const taskStatusConfig = {
  queued: {
    icon: Clock3,
    label: "Queued",
    className: "border-amber-500/20 bg-amber-500/8 text-amber-700 dark:text-amber-300",
  },
  dispatched: {
    icon: Loader2,
    label: "Starting",
    className: "border-sky-500/20 bg-sky-500/8 text-sky-700 dark:text-sky-300",
  },
  running: {
    icon: Loader2,
    label: "Running",
    className: "border-brand/20 bg-brand/8 text-brand",
  },
} as const;

type Variant = "board" | "list";

export function IssueTaskStatusBadge({
  issue,
  variant,
}: {
  issue: Issue;
  variant: Variant;
}) {
  const enabled = issue.assignee_type === "agent";
  const snapshot = useIssueTaskStore((state) => state.byIssueId[issue.id]);
  const registerIssue = useIssueTaskStore((state) => state.registerIssue);
  const unregisterIssue = useIssueTaskStore((state) => state.unregisterIssue);
  const refreshIssue = useIssueTaskStore((state) => state.refreshIssue);

  useEffect(() => {
    if (!enabled) return;
    registerIssue(issue.id);
    if (!snapshot?.loaded) {
      void refreshIssue(issue.id);
    }

    return () => {
      unregisterIssue(issue.id);
    };
  }, [enabled, issue.id, refreshIssue, registerIssue, snapshot?.loaded, unregisterIssue]);

  if (!enabled || !snapshot?.task) return null;

  const config = taskStatusConfig[snapshot.task.status as keyof typeof taskStatusConfig];
  if (!config) return null;

  const Icon = config.icon;
  const progressLabel =
    snapshot.summary ||
    (snapshot.step && snapshot.total
      ? `Step ${snapshot.step}/${snapshot.total}`
      : config.label);

  if (variant === "list") {
    return (
      <span
        className={cn(
          "inline-flex max-w-56 items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] font-medium",
          config.className,
        )}
      >
        {snapshot.task.agent_id ? <Bot className="h-3 w-3 shrink-0" /> : null}
        <Icon className={cn("h-3 w-3 shrink-0", snapshot.task.status !== "queued" ? "animate-spin" : "")} />
        <span className="truncate">{progressLabel}</span>
      </span>
    );
  }

  return (
    <div
      className={cn(
        "mt-2 rounded-md border px-2 py-1.5",
        config.className,
      )}
    >
      <div className="flex items-center gap-1.5 text-[11px] font-medium">
        {snapshot.task.agent_id ? <Bot className="h-3 w-3 shrink-0" /> : null}
        <Icon className={cn("h-3 w-3 shrink-0", snapshot.task.status !== "queued" ? "animate-spin" : "")} />
        <span>{config.label}</span>
      </div>
      {progressLabel !== config.label ? (
        <p className="mt-1 line-clamp-2 text-[11px] text-muted-foreground">
          {progressLabel}
        </p>
      ) : null}
    </div>
  );
}