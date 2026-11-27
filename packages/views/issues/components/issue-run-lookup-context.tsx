"use client";

import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { skipToken, useQuery, type QueryKey, type UseQueryOptions } from "@tanstack/react-query";
import { issueTasksOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import { commentSupplementReceipts } from "@multica/core/issues/run-steering";
import type { AgentTask, TimelineEntry } from "@multica/core/types";

export interface DeliveredSteer {
  id: string;
  actorType: string;
  actorId: string;
  content: string;
  deliveredAt: string;
}

const NO_STEERS: DeliveredSteer[] = [];

export function buildDeliveredSteersByTask(entries: readonly TimelineEntry[]): Map<string, DeliveredSteer[]> {
  const byTask = new Map<string, DeliveredSteer[]>();
  for (const entry of entries) {
    if (entry.type !== "comment") continue;
    const seen = new Set<string>();
    for (const receipt of commentSupplementReceipts(entry)) {
      if (seen.has(receipt.task_id) || receipt.status !== "delivered" || !receipt.delivered_at) continue;
      seen.add(receipt.task_id);
      const steers = byTask.get(receipt.task_id) ?? [];
      steers.push({
        id: entry.id,
        actorType: entry.actor_type,
        actorId: entry.actor_id,
        content: entry.content ?? "",
        deliveredAt: receipt.delivered_at,
      });
      byTask.set(receipt.task_id, steers);
    }
  }
  return byTask;
}

export function buildWakeupIdByTask(tasks: readonly AgentTask[]): Map<string, string | undefined> {
  return new Map(tasks.map((task) => [task.id, task.wakeup_id]));
}

export interface IssueRunLookup {
  deliveredSteersByTask: ReadonlyMap<string, DeliveredSteer[]>;
  wakeupIdByTask: ReadonlyMap<string, string | undefined>;
}

const IssueRunLookupContext = createContext<IssueRunLookup | null>(null);

export function useIssueRunLookup(): IssueRunLookup | null {
  return useContext(IssueRunLookupContext);
}

export function IssueRunLookupProvider({ timeline, tasks, children }: {
  timeline: readonly TimelineEntry[];
  tasks: readonly AgentTask[];
  children: ReactNode;
}) {
  const deliveredSteersByTask = useMemo(() => buildDeliveredSteersByTask(timeline), [timeline]);
  const wakeupIdByTask = useMemo(() => buildWakeupIdByTask(tasks), [tasks]);
  const value = useMemo(() => ({ deliveredSteersByTask, wakeupIdByTask }), [deliveredSteersByTask, wakeupIdByTask]);
  return <IssueRunLookupContext.Provider value={value}>{children}</IssueRunLookupContext.Provider>;
}

const DISABLED_LOOKUP_QUERY_KEY = ["__issue_run_lookup_disabled__"] as const;
const noSteers = () => NO_STEERS;
const noWakeupId = () => undefined;

/**
 * Reads a task's delivered steers from the issue-level lookup. The optional
 * issue id is only for standalone rendering without the provider.
 */
export function useDeliveredSteersForTask(taskId: string, issueId = ""): DeliveredSteer[] {
  const lookup = useIssueRunLookup();
  const select = useCallback(
    (entries: TimelineEntry[]) => buildDeliveredSteersByTask(entries).get(taskId) ?? NO_STEERS,
    [taskId],
  );
  const timelineOptions = issueTimelineOptions(issueId);
  const { data } = useQuery({
    ...timelineOptions,
    queryKey: lookup ? DISABLED_LOOKUP_QUERY_KEY : timelineOptions.queryKey,
    queryFn: lookup ? skipToken : timelineOptions.queryFn,
    enabled: false,
    select: lookup ? noSteers : select,
  } as UseQueryOptions<TimelineEntry[], Error, DeliveredSteer[], QueryKey>);
  if (lookup) return lookup.deliveredSteersByTask.get(taskId) ?? NO_STEERS;
  return data ?? NO_STEERS;
}

export function useTaskWakeupId(issueId: string, taskId: string): string | undefined {
  const lookup = useIssueRunLookup();
  const select = useCallback(
    (tasks: AgentTask[]) => tasks.find((task) => task.id === taskId)?.wakeup_id,
    [taskId],
  );
  const tasksOptions = issueTasksOptions(issueId);
  const { data } = useQuery({
    ...tasksOptions,
    queryKey: lookup ? DISABLED_LOOKUP_QUERY_KEY : tasksOptions.queryKey,
    queryFn: lookup ? skipToken : tasksOptions.queryFn,
    enabled: !lookup && !!issueId,
    select: lookup ? noWakeupId : select,
  } as UseQueryOptions<AgentTask[], Error, string | undefined, QueryKey>);
  if (lookup) return lookup.wakeupIdByTask.get(taskId);
  return data;
}
