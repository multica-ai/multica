"use client";

import { useCallback, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { issueTasksOptions, issueWakeupsOptions } from "@multica/core/issues";
import type { AgentTask, IssueWakeup } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { useWakeupText } from "./wakeup-presentation";

/** The wakeup that started a run, when the issue still has it. */
export function useRunWakeup(issueId: string, wakeupId?: string): IssueWakeup | undefined {
  const wsId = useWorkspaceId();
  const select = useCallback((rules: IssueWakeup[]) => rules.find((w) => w.id === wakeupId), [wakeupId]);
  const { data } = useQuery({ ...issueWakeupsOptions(wsId, issueId), enabled: !!wsId && !!issueId && !!wakeupId, select });
  return data;
}

/**
 * "Triggered by wakeup · <condition>" on a comment an agent posted from a run
 * that a wakeup started. Renders nothing for other comments.
 */
export function WakeupSourceChip({ issueId, taskId }: { issueId: string; taskId: string }) {
  const select = useCallback((tasks: AgentTask[]) => tasks.find((task) => task.id === taskId)?.wakeup_id, [taskId]);
  const { data: wakeupId } = useQuery({ ...issueTasksOptions(issueId), select });
  return wakeupId ? <WakeupSourceChipLabel issueId={issueId} wakeupId={wakeupId} /> : null;
}

function WakeupSourceChipLabel({ issueId, wakeupId }: { issueId: string; wakeupId: string }) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const rule = useRunWakeup(issueId, wakeupId);
  const label = rule
    ? t(($) => $.execution_log.trigger_wakeup, { condition: text.trigger(rule) })
    : t(($) => $.wakeups.triggered_by_wakeup);
  return (
    <span
      className="inline-flex min-w-0 max-w-64 items-center gap-1 rounded-sm bg-muted px-1.5 py-0.5 text-micro text-muted-foreground"
      title={label}
    >
      <Bell className="size-3 shrink-0" aria-hidden="true" />
      <span className="truncate">{label}</span>
    </span>
  );
}

/**
 * A wakeup run's trigger label with the rule's condition, e.g. "Wakeup · When
 * a linked PR's CI finishes". Mounted only for wakeup runs, so other run
 * lists never load the issue's rules.
 */
export function WakeupRunLabel({
  task,
  fallback,
  render,
}: {
  task: AgentTask;
  fallback: string;
  render: (label: string) => ReactNode;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const rule = useRunWakeup(task.issue_id, task.wakeup_id);
  return render(rule ? t(($) => $.execution_log.trigger_wakeup, { condition: text.trigger(rule) }) : fallback);
}
