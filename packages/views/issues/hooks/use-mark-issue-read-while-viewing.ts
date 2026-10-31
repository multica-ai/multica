import { useCallback, useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueUnreadOptions, useMarkIssueRead } from "@multica/core/issues/unread";
import type { IssueUnread } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";

/**
 * Clears the viewer's unread mark while the issue is on screen: when it is
 * opened, and when a run ends while it stays open. A mark the viewer set by
 * hand while the issue is open is kept, or "Mark as unread" would undo itself.
 * Nothing is cleared while the app is in the background.
 */
export function useMarkIssueReadWhileViewing(wsId: string, issueId: string) {
  const foreground = useAppForeground();
  const select = useCallback(
    (items: IssueUnread[]) => items.find((item) => item.issue_id === issueId) ?? null,
    [issueId],
  );
  const { data: unread = null, isSuccess } = useQuery({
    ...issueUnreadOptions(wsId),
    select,
    enabled: !!issueId,
  });
  const { mutate: markRead } = useMarkIssueRead(wsId);
  const viewedIssueId = useRef<string | null>(null);

  useEffect(() => {
    if (!issueId || !isSuccess || !foreground) return;
    const opening = viewedIssueId.current !== issueId;
    viewedIssueId.current = issueId;
    if (unread && (opening || unread.outcome !== "manual")) markRead(issueId);
  }, [issueId, isSuccess, foreground, unread, markRead]);
}
