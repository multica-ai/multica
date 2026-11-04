import { useCallback, useEffect, useRef, useState } from "react";
import { useInboxUnreadCount, useMarkIssueInboxRead } from "@multica/core/inbox";
import { useWSEvent } from "@multica/core/realtime";
import type { InboxNewPayload } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";

/**
 * Reads the viewer's inbox notifications on an issue while it is on screen
 * outside the inbox: when it is opened, and when a notification on it arrives
 * while it stays open. The inbox already clears an issue's Unread mark when it
 * opens the issue, so with this, reading an issue in either place reads it in
 * both (MUL-8588). Nothing is read while the app is in the background.
 *
 * The inbox passes `enabled: false` for the issue it has open. It reads the
 * row it selected itself, and keeps a row the reader marked unread there.
 *
 * Nothing is sent while the workspace has no unread notifications, so opening
 * issues at inbox zero costs no request. Each opening and each arrival is
 * written at most once: a failed write is not retried here, and the next
 * arrival or reopening tries again.
 */
export function useMarkIssueInboxReadWhileViewing(wsId: string, issueId: string, enabled = true) {
  const foreground = useAppForeground();
  const hasUnreadInbox = useInboxUnreadCount(wsId) > 0;
  const { mutate: markRead } = useMarkIssueInboxRead(wsId);
  // Notifications on this issue that arrived since it opened. Each one is a
  // new write; the unread summary alone cannot tell which issue it was for.
  const [arrivals, setArrivals] = useState(0);
  const written = useRef<string | null>(null);

  const onInboxNew = useCallback(
    (payload: unknown) => {
      const item = (payload as Partial<InboxNewPayload> | null)?.item;
      if (issueId && item?.issue_id === issueId && !item.read) setArrivals((n) => n + 1);
    },
    [issueId],
  );
  useWSEvent("inbox:new", onInboxNew);

  useEffect(() => {
    if (!enabled || !issueId || !foreground || !hasUnreadInbox) return;
    const key = `${issueId}|${arrivals}`;
    if (written.current === key) return;
    written.current = key;
    markRead(issueId);
  }, [enabled, issueId, foreground, hasUnreadInbox, arrivals, markRead]);
}
