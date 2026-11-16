import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { countUnreadChatMessages } from "@multica/core/chat/unread";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";

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
  const { data: sessions } = useQuery({
    ...chatSessionsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const count = inboxCount + (wsId ? countUnreadChatMessages(sessions) : 0);
  useEffect(() => {
    getDesktopAPI()?.setUnreadBadge?.(count);
  }, [count]);
}
