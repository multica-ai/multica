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

const EMPTY_INBOX: InboxItem[] = [];
const EMPTY_TASKS: AgentTask[] = [];

/**
 * The live Inbox as the Work panel lists it: one row per issue, newest first,
 * under the workspace's Inbox filters (the same ones the phone's Inbox page
 * sets).
 *
 * `openKey` is the notification open beside the list (`issue_id ?? id`). As
 * on the Inbox page, it survives the unread and agent filters: opening it
 * reads it, and replying can start or stop an agent, and neither should take
 * the row the user is working in out of the list.
 *
 * `enabled: false` reads nothing, for a caller that may not need the list.
 */
export function useFilteredInboxRows(wsId: string, openKey: string | null, enabled = true) {
  const { data: raw = EMPTY_INBOX } = useQuery({ ...inboxListOptions(wsId), enabled: enabled && !!wsId });
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
  const rows = useMemo(
    () =>
      filterInboxItems(notifications, filters, {
        activeIssueIds: agentIssueIds,
        pinnedUnreadKey: openKey,
        pinnedAgentKey: openKey,
      }),
    [notifications, filters, agentIssueIds, openKey],
  );

  return {
    rows,
    notifications,
    filtered: inboxFilterCount(filters) > 0,
    priorityFilterSupport,
    issueFieldsSupport,
    activeIssueIds,
  };
}
