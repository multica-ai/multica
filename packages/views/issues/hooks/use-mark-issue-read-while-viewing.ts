import { useCallback, useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { issueOpenMayReadInbox } from "@multica/core/inbox/queries";
import { issueUnreadOptions, useMarkIssueRead } from "@multica/core/issues/unread";
import type { IssueUnread } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";

/**
 * Clears the viewer's unread mark while the issue is on screen: when it is
 * opened, and when a run ends while it stays open. A mark the viewer set by
 * hand while the issue is open is kept, or "Mark as unread" would undo itself.
 * Nothing is cleared while the app is in the background.
 *
 * The same write reads the issue's informational Inbox notifications, so
 * opening an issue also writes when only the Inbox has something to read.
 * That happens once per opening: an Inbox item marked unread by hand while the
 * issue is open stays unread until it is reopened or another run ends.
 *
 * Each unread mark is cleared at most once per viewing. A failed write rolls
 * the cache back to that same mark; trying again on it would loop. The
 * mutation's own retries cover transient failures, and a newer mark (another
 * run ending) or reopening the issue tries again.
 */
export function useMarkIssueReadWhileViewing(wsId: string, issueId: string) {
  const qc = useQueryClient();
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
  const clearedMark = useRef<string | null>(null);

  useEffect(() => {
    if (!issueId || !isSuccess || !foreground) return;
    const opening = viewedIssueId.current !== issueId;
    viewedIssueId.current = issueId;
    if (opening) clearedMark.current = null;
    const mark = unread && (opening || unread.outcome !== "manual") ? `${unread.outcome}|${unread.created_at}` : null;
    if (mark !== null && clearedMark.current !== mark) {
      clearedMark.current = mark;
      markRead(issueId);
    } else if (opening && issueOpenMayReadInbox(qc, wsId, issueId)) {
      markRead(issueId);
    }
  }, [qc, wsId, issueId, isSuccess, foreground, unread, markRead]);
}
