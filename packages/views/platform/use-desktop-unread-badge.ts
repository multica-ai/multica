import { useEffect } from "react";
// CEREBRO-PATCH(inbox-badge-thread-aware): FIR-2382 — thread-aware unread count so the OS dock badge matches the inbox list.
import { useCerebroInboxUnreadCount } from "@multica/cerebro-inbox";

type BadgeCapableAPI = {
  setUnreadBadge?: (count: number) => void;
};

function getDesktopAPI(): BadgeCapableAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: BadgeCapableAPI }).desktopAPI;
}

/**
 * Mirror the inbox unread count onto the OS dock/taskbar badge. No-op on web
 * (no `desktopAPI`) and on the login screen (no workspace ⇒ count defaults
 * to 0, which clears any stale badge from a previous session).
 */
export function useDesktopUnreadBadge(wsId: string | null | undefined): void {
  const count = useCerebroInboxUnreadCount(wsId);
  useEffect(() => {
    getDesktopAPI()?.setUnreadBadge?.(count);
  }, [count]);
}
