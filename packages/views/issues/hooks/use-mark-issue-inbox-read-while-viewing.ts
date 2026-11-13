import { useCallback, useEffect, useRef, useState } from "react";
import { useMarkIssueInboxRead } from "@multica/core/inbox";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { InboxNewPayload } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";

/**
 * Reads the viewer's inbox notifications on an issue while it is on screen
 * outside the inbox: when it is opened, when a notification on it arrives
 * while it stays open, and after the socket reconnects (an `inbox:new` sent
 * while it was down is never replayed). The inbox already clears an issue's
 * Unread mark when it opens the issue, so with this, reading an issue in
 * either place reads it in both (MUL-8588). Nothing is read while the app is
 * in the background.
 *
 * The inbox passes `enabled: false` for the issue it has open. It reads the
 * row it selected itself, and keeps a row the reader marked unread there.
 *
 * The deduplicated badge can be zero while older notifications are unread,
 * so each opening, arrival and reconnect is written regardless of the badge.
 * Each is written at most once: a failed write is not retried here, and the
 * next one of those tries again.
 */
export function useMarkIssueInboxReadWhileViewing(wsId: string, issueId: string, enabled = true) {
  const foreground = useAppForeground();
  const { mutate: markRead } = useMarkIssueInboxRead(wsId);
  // Bumped by every reason to read again since the issue opened. The unread
  // summary alone cannot tell which issue a new notification is for.
  const [generation, setGeneration] = useState(0);
  const written = useRef<string | null>(null);

  const onInboxNew = useCallback(
    (payload: unknown) => {
      const item = (payload as Partial<InboxNewPayload> | null)?.item;
      if (issueId && item?.issue_id === issueId && !item.read) setGeneration((n) => n + 1);
    },
    [issueId],
  );
  useWSEvent("inbox:new", onInboxNew);

  // An inbox:new sent while the socket was down is never replayed.
  const onReconnect = useCallback(() => setGeneration((n) => n + 1), []);
  useWSReconnect(onReconnect);

  useEffect(() => {
    if (!enabled || !issueId) {
      written.current = null;
      return;
    }
    if (!foreground) return;
    const key = `${wsId}|${issueId}|${generation}`;
    if (written.current === key) return;
    written.current = key;
    markRead(issueId);
  }, [enabled, wsId, issueId, foreground, generation, markRead]);
}
