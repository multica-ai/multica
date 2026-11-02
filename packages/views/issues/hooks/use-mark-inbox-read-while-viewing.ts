import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMarkIssueInboxRead } from "@multica/core/inbox/mutations";
import {
  inboxKeys,
  inboxUnreadSummaryEnabled,
  inboxUnreadSummaryOptions,
  isInboxItemUnread,
  unreadCountForWorkspace,
} from "@multica/core/inbox/queries";
import type { InboxItem, InboxWorkspaceUnread } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";

/**
 * Settles the issue's Inbox notifications when it is opened, from any surface
 * — the Work sidebar, search, a link — so the same event is never cleared
 * twice. Runs once per opening, in the foreground: a "Mark as unread" made
 * while the issue stays open is kept.
 *
 * Skips the request when nothing can be unread: the workspace's unread count
 * is zero, or the loaded Inbox list holds no unread row for this issue.
 */
export function useMarkInboxReadWhileViewing(wsId: string, issueId: string) {
  const qc = useQueryClient();
  const foreground = useAppForeground();
  const { data: workspaceUnread, isSuccess } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: inboxUnreadSummaryEnabled(wsId),
    select: (summary: InboxWorkspaceUnread[]) => unreadCountForWorkspace(summary, wsId),
  });
  const { mutate: markRead } = useMarkIssueInboxRead(wsId);
  const viewedIssueId = useRef<string | null>(null);

  useEffect(() => {
    if (!issueId || !foreground || !isSuccess) return;
    if (viewedIssueId.current === issueId) return;
    viewedIssueId.current = issueId;
    if (workspaceUnread === 0) return;
    const list = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    if (list && !list.some((item) => item.issue_id === issueId && !item.archived && isInboxItemUnread(item))) {
      return;
    }
    markRead(issueId);
  }, [issueId, foreground, isSuccess, workspaceUnread, qc, wsId, markRead]);
}
