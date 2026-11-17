"use client";

import type { AgentTask } from "@multica/core/types";
import { WakeupRunLabel } from "./wakeup-source-chip";

/**
 * A run's trigger, with a wakeup run naming its rule's condition ("Wakeup ·
 * When a linked PR's CI finishes") the way the execution log's rows do. The
 * rule lookup mounts only for wakeup runs.
 */
export function RunTriggerLabel({
  task,
  fallback,
  children,
}: {
  task: AgentTask;
  fallback: string;
  children: (label: string) => React.ReactNode;
}) {
  if (!task.wakeup_id) return <>{children(fallback)}</>;
  return <WakeupRunLabel task={task} fallback={fallback} render={children} />;
}
