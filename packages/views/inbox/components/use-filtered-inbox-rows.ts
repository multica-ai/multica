"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { inboxListOptions } from "@multica/core/inbox/queries";
import {
  effectiveInboxFilters,
  filterInboxItems,
  inboxFilterCount,
  inboxIssueFieldsSupport,
  inboxPriorityFilterSupport,
  useInboxFilters,
} from "@multica/core/inbox/filter-store";
import type { AgentTask, InboxItem } from "@multica/core/types";
import { deriveIssueSurfaceActivity } from "../../issues/surface/activity";
import { useKeptOpenInboxRow } from "./use-kept-open-inbox-row";

/** The Work panel's Inbox section, and the issue header that works through it. */
export const INBOX_SECTION_LIST = "section";

const EMPTY_INBOX: InboxItem[] = [];

/** Where the live Inbox list stands: only a `loaded` list may read as empty. */
export type InboxListLoad = "loading" | "loaded" | "failed";

/**
 * A list that has loaded stays `loaded` when a later refresh fails: its rows
 * stay on screen rather than the Inbox emptying. One that failed stays
 * `failed` while it is tried again: a new attempt resets the query to pending,
 * and its retries can take seconds.
 */
export function inboxListLoad(query: {
  data: unknown;
  isError: boolean;
  isFetching: boolean;
  errorUpdateCount: number;
}): InboxListLoad {
  if (query.data !== undefined) return "loaded";
  return query.isError || (query.errorUpdateCount > 0 && query.isFetching) ? "failed" : "loading";
}
const EMPTY_TASKS: AgentTask[] = [];

/**
 * The live Inbox as the Work panel lists it: one row per issue, newest first,
 * under the workspace's Inbox filters (the same ones the phone's Inbox page
 * sets).
 *
 * `openKey` is the notification open beside the list (`issue_id ?? id`). As
 * on the Inbox page, it survives the unread and agent filters: opening it
 * reads it, and replying can start or stop an agent, and neither should take
 * the row the user is working in out of the list. Anything else that takes
 * it out leaves it in place as `kept` (`keepOpenInboxRow`).
 *
 * `enabled: false` reads nothing, for a caller that may not need the list.
 */
export function useFilteredInboxRows(wsId: string, openKey: string | null, enabled = true) {
  const query = useQuery({ ...inboxListOptions(wsId), enabled: enabled && !!wsId });
  const raw = query.data ?? EMPTY_INBOX;
  const stored = useInboxFilters(wsId);
  // Every notification, not one per issue: the notification filters match any
  // notification of an issue.
  const notifications = useMemo(() => raw.filter((item) => !item.archived), [raw]);
  const priorityFilterSupport = inboxPriorityFilterSupport(raw);
  const issueFieldsSupport = inboxIssueFieldsSupport(raw);
  const filters = useMemo(
    () => effectiveInboxFilters(stored, { priority: priorityFilterSupport, issueFields: issueFieldsSupport }),
    [stored, priorityFilterSupport, issueFieldsSupport],
  );
  // "Active" is the running-or-queued rule the Inbox page and the row badges use.
  const { data: tasks = EMPTY_TASKS } = useQuery({ ...agentTaskSnapshotOptions(wsId), enabled: enabled && !!wsId });
  const activeIssueIds = useMemo(
    () => new Set(deriveIssueSurfaceActivity(tasks).activityByIssueId.keys()),
    [tasks],
  );
  // Only an agent filter reads the snapshot, so task churn leaves the list alone while it is off.
  const agentIssueIds = filters.agent ? activeIssueIds : undefined;
  const filtered = useMemo(
    () =>
      filterInboxItems(notifications, filters, {
        activeIssueIds: agentIssueIds,
        pinnedUnreadKey: openKey,
        pinnedAgentKey: openKey,
      }),
    [notifications, filters, agentIssueIds, openKey],
  );
  const { rows, kept } = useKeptOpenInboxRow(
    wsId,
    INBOX_SECTION_LIST,
    filtered,
    notifications,
    openKey,
    filters,
    enabled,
  );

  return {
    rows,
    kept,
    /**
     * Whether the list has loaded. Only a list that loaded may read as empty:
     * one still loading or that failed shows no rows without meaning there
     * are none. A failed refresh of a loaded list keeps it as it was.
     */
    load: inboxListLoad(query),
    retry: () => void query.refetch(),
    retrying: query.isFetching,
    notifications,
    filtered: inboxFilterCount(filters) > 0,
    priorityFilterSupport,
    issueFieldsSupport,
    activeIssueIds,
  };
}
