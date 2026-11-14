"use client";

import { useEffect, useMemo, useState } from "react";
import { useQueries, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import {
  inboxFilterCount,
  inboxIssueMatches,
  inboxIssueProjection,
  type InboxFilters,
} from "@multica/core/inbox/filter-store";
import { inboxKeys } from "@multica/core/inbox/queries";
import {
  buildInboxSections,
  nextInboxWake,
  type InboxSections,
} from "@multica/core/inbox/sections";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { AgentTask, InboxItem, Issue } from "@multica/core/types";

const EMPTY_TASKS: AgentTask[] = [];
const issuesOf = (results: UseQueryResult<Issue>[]) => ({
  issues: results.map((result) => result.data),
  pending: results.some((result) => result.isPending),
});

export interface InboxSectionsState extends InboxSections {
  /**
   * "Working" is not settled: the task snapshot has not answered, or under a
   * filter an issue a row is tested against has not loaded. Until then a link
   * to an issue that is only listed there cannot be told from a broken one.
   */
  workingPending: boolean;
}
// The longest delay a timer takes; a snooze further out re-arms when it fires.
const MAX_TIMER_MS = 2_147_000_000;

/**
 * The current time, moved forward when the next snooze in `items` runs out.
 * Nothing arrives from the server when a snooze simply elapses, so the list
 * has to notice by itself that an issue is back, and the badge is re-read
 * with it so the two agree.
 */
function useInboxClock(items: readonly InboxItem[]): number {
  const qc = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  const nextWake = useMemo(() => nextInboxWake(items, now), [items, now]);
  useEffect(() => {
    if (nextWake == null) return;
    const delay = Math.min(Math.max(nextWake - Date.now(), 0) + 50, MAX_TIMER_MS);
    const timer = setTimeout(() => {
      setNow(Date.now());
      if (Date.now() >= nextWake) void qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
    }, delay);
    return () => clearTimeout(timer);
  }, [nextWake, qc]);
  return now;
}

/**
 * The Inbox list split into its sections.
 *
 * `rows` is the list as filtered, one notification per issue; `items` is every
 * active notification. "Working" is read from the workspace's task snapshot,
 * which realtime already keeps fresh.
 *
 * `filters` narrows "Working" like every other section. A run carries none of
 * its issue's fields, so the row is tested against the issue itself, the same
 * cache entry the row shows its title from, and is left out until that has
 * loaded.
 */
export function useInboxSections(
  wsId: string,
  rows: readonly InboxItem[],
  items: readonly InboxItem[],
  { filters, openKey = null }: { filters: InboxFilters; openKey?: string | null },
): InboxSectionsState {
  const userId = useAuthStore((s) => s.user?.id);
  const { data: tasks = EMPTY_TASKS, isPending: tasksPending } = useQuery(agentTaskSnapshotOptions(wsId));
  const now = useInboxClock(items);
  const sections = useMemo(
    () => buildInboxSections(rows, items, now, { working: { tasks, userId }, openKey }),
    [rows, items, now, tasks, userId, openKey],
  );
  const filtered = inboxFilterCount(filters) > 0;
  const workingIssues = useQueries({
    queries: filtered ? sections.working.map((row) => issueDetailOptions(wsId, row.issueId)) : [],
    combine: issuesOf,
  });
  return useMemo(() => {
    if (!filtered) return { ...sections, workingPending: tasksPending };
    const at = new Date(now);
    return {
      ...sections,
      working: sections.working.filter((_row, index) => {
        const issue = workingIssues.issues[index];
        return !!issue && inboxIssueMatches(inboxIssueProjection(issue), filters, at);
      }),
      workingPending: tasksPending || workingIssues.pending,
    };
  }, [sections, filtered, workingIssues, filters, now, tasksPending]);
}
