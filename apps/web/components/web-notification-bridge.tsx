"use client";

import { useEffect, useRef } from "react";
import {
  registerSystemNotificationClickHandler,
  type SystemNotificationPayload,
} from "@multica/core/platform";
import { paths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";

/**
 * Routes browser notification clicks to the source workspace's inbox, focused
 * on the clicked item. The web counterpart of the desktop `DesktopInboxBridge`:
 * desktop receives the click via Electron IPC, web wires it through the
 * Notification API's `onclick` (registered here into the core singleton).
 *
 * The route uses the `slug` the notification was emitted with — the SOURCE
 * workspace — not the active one, so a click always opens the right inbox even
 * after the user switches workspaces (#3766). An empty slug (unresolved
 * source) is ignored. Marking the row read is handled by InboxPage's
 * selected-item effect, which covers the `?issue=` URL-param path.
 */
export function WebNotificationBridge() {
  const { push } = useNavigation();
  // The adapter identity changes with the current route; the ref keeps the
  // registered click handler stable while always calling the latest push.
  const pushRef = useRef(push);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);

  useEffect(() => {
    registerSystemNotificationClickHandler(
      ({ slug, issueKey }: SystemNotificationPayload) => {
        if (!slug) return;
        const inboxPath = `${paths.workspace(slug).inbox()}?issue=${encodeURIComponent(issueKey)}`;
        pushRef.current(inboxPath);
      },
    );
    return () => registerSystemNotificationClickHandler(null);
  }, []);

  return null;
}
