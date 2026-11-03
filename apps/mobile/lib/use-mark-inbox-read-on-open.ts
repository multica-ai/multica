/**
 * Settles an issue's inbox notifications when its screen opens, so opening it
 * from a link, search, or the issue list clears the inbox too — the same
 * event is never cleared twice. Mirrors web's useMarkInboxReadWhileViewing
 * (packages/views/issues/hooks/use-mark-inbox-read-while-viewing.ts).
 *
 * Runs once per opened issue. Skips the request only on fresh evidence that
 * nothing can be unread: a settled workspace unread count of zero, or an
 * up-to-date inbox list with no unread row for this issue. A refetching
 * summary is waited for and a stale list is ignored.
 */
import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import { isInboxItemUnread } from "@multica/core/inbox/section";
import { useMarkIssueInboxRead } from "@/data/mutations/inbox";
import { inboxKeys, inboxUnreadSummaryOptions } from "@/data/queries/inbox";

export function useMarkInboxReadOnOpen(
  wsId: string | null,
  issueId: string | undefined,
) {
  const qc = useQueryClient();
  const { data: workspaceUnread, isSuccess, isFetching } = useQuery({
    ...inboxUnreadSummaryOptions(wsId),
    select: (summary) =>
      summary.find((s) => s.workspace_id === wsId)?.count ?? 0,
  });
  const { mutate: markRead } = useMarkIssueInboxRead();
  const openedIssueId = useRef<string | null>(null);

  useEffect(() => {
    if (!wsId || !issueId || !isSuccess || isFetching) return;
    if (openedIssueId.current === issueId) return;
    openedIssueId.current = issueId;
    if (workspaceUnread === 0) return;
    const listQuery = qc.getQueryCache().find<InboxItem[]>({
      queryKey: inboxKeys.list(wsId),
      exact: true,
    });
    const list =
      listQuery && !listQuery.isStale() ? listQuery.state.data : undefined;
    if (
      list &&
      !list.some(
        (item) =>
          item.issue_id === issueId && !item.archived && isInboxItemUnread(item),
      )
    ) {
      return;
    }
    markRead(issueId);
  }, [wsId, issueId, isSuccess, isFetching, workspaceUnread, qc, markRead]);
}
