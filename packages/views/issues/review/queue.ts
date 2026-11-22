import { todayDateOnly } from "@multica/core/issues/date";
import { api } from "@multica/core/api";
import type { Issue, IssueTableQuerySpec, IssueTableRowsResponse, IssueTableGroupsRequest, IssueTableGroupsResponse } from "@multica/core/types";

export class ReviewQueueChanged extends Error {}

/** Read the same concrete status branch as List, including its unloaded tails.
 * A fingerprint identifies the query, not a transaction snapshot. Drift fails
 * closed instead of turning a truncated response into "no next issue". */
export async function readReviewQueue(
  query: IssueTableQuerySpec,
  read: (request: {
    query: IssueTableQuerySpec;
    group: { kind: "status" };
    group_key: string;
    hierarchy: { enabled: false };
    parent_id: null;
    page: { limit: number; cursor: string | null };
  }) => Promise<IssueTableRowsResponse>,
  readGroups: (request: IssueTableGroupsRequest) => Promise<IssueTableGroupsResponse>,
): Promise<Issue[]> {
  const issues: Issue[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let cursor: string | null = null;
  let fingerprint: string | null = null;
  const count = async () => {
    let cursor: string | null = null;
    let expected: number | null = null;
    const seen = new Set<string>();
    do {
      if (cursor && seen.has(cursor)) throw new ReviewQueueChanged();
      if (cursor) seen.add(cursor);
      const page = await readGroups({ query, group: { kind: "status" }, page: { limit: 100, cursor } });
      if (!page.query_fingerprint || (fingerprint && fingerprint !== page.query_fingerprint)) throw new ReviewQueueChanged();
      fingerprint = page.query_fingerprint;
      for (const group of page.groups) {
        if (group.key !== "status:in_review") continue;
        if (expected !== null) throw new ReviewQueueChanged();
        expected = group.count;
      }
      cursor = page.next_cursor;
    } while (cursor);
    return expected ?? 0;
  };
  const total = await count();
  do {
    if (cursor && cursors.has(cursor)) throw new ReviewQueueChanged();
    if (cursor) cursors.add(cursor);
    const page = await read({
      query, group: { kind: "status" }, group_key: "status:in_review",
      hierarchy: { enabled: false }, parent_id: null, page: { limit: 50, cursor },
    });
    fingerprint ??= page.query_fingerprint;
    if (
      !fingerprint || page.query_fingerprint !== fingerprint ||
      page.group_key !== "status:in_review" || page.parent_id !== null ||
      page.branch_total !== page.rows.length
    ) throw new ReviewQueueChanged();
    for (const { issue } of page.rows) {
      if (issue.status !== "in_review" || ids.has(issue.id)) {
        throw new ReviewQueueChanged();
      }
      ids.add(issue.id);
      issues.push(issue);
    }
    cursor = page.next_cursor;
  } while (cursor);
  if (issues.length !== total || await count() !== total) throw new ReviewQueueChanged();
  return issues;
}

/** Keep the write-time order after the completed row disappears or re-sorts.
 * Revalidate membership against every fresh page, then wrap at the end. */
export function nextReviewIssue(before: Issue[], currentId: string, fresh: Issue[]): Issue | null {
  const index = before.findIndex((issue) => issue.id === currentId);
  if (index < 0) throw new ReviewQueueChanged();
  const live = new Map(fresh.map((issue) => [issue.id, issue]));
  for (const candidate of [...before.slice(index + 1), ...before.slice(0, index)]) {
    const issue = live.get(candidate.id);
    if (issue) return issue;
  }
  // Only new arrivals remain: they become the next action's new queue.
  return fresh.find((issue) => issue.id !== currentId) ?? null;
}

/** Refresh server-owned projections without changing the source predicate.
 * Unread lists intentionally retain read items for this source session, exactly
 * as the surface controller does; newly unread rows join that retained set. */
export async function refreshReviewQuery(query: IssueTableQuerySpec): Promise<IssueTableQuerySpec> {
  const filters = { ...query.filters };
  if (filters.working_issue_ids !== undefined) {
    const working = await api.getWorkspaceWorkingAgents("issue", query.scope.kind === "my" ? query.scope.relation : undefined);
    filters.working_issue_ids = [...new Set(working.flatMap((agent) => agent.issue_ids))];
  }
  if (filters.unread_issue_ids !== undefined) {
    const unread = await api.listIssueUnread();
    filters.unread_issue_ids = [...new Set([...filters.unread_issue_ids, ...unread.map((item) => item.issue_id)])].sort();
  }
  if (filters.today) filters.today = todayDateOnly();
  return { ...query, filters };
}
