import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { chatSessionSummaryOptions } from "@multica/core/chat/queries";
import { countUnreadChatMessages } from "@multica/core/chat/unread";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useIsGuest } from "@multica/core/permissions";

type BadgeCapableAPI = {
  setUnreadBadge?: (count: number) => void;
};

function getDesktopAPI(): BadgeCapableAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: BadgeCapableAPI }).desktopAPI;
}

/**
 * Mirror Inbox + Chat unread counts onto the OS dock/taskbar badge. No-op on web
 * (no `desktopAPI`) and on the login screen (no workspace ⇒ count defaults
 * to 0, which clears any stale badge from a previous session). The badge is
 * on screen while the window is minimized, so it keeps the summary live then.
 */
export function useDesktopUnreadBadge(wsId: string | null | undefined): void {
  const inboxCount = useInboxUnreadCount(wsId, { whileHidden: true });
  const memberWsId = useIsGuest(wsId) ? null : wsId;
  const { data: summary } = useQuery({
    ...chatSessionSummaryOptions(memberWsId ?? ""),
    enabled: !!memberWsId,
  });
  const chatCount = countUnreadChatMessages(
    Object.entries(summary?.unread_by_session ?? {}).map(([id, unread_count]) => ({ id, unread_count })),
  );
  const count = inboxCount + chatCount;
  useEffect(() => {
    getDesktopAPI()?.setUnreadBadge?.(count);
  }, [count]);
}
