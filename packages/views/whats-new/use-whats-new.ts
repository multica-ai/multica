"use client";

import { useCallback, useMemo } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { createLogger } from "@multica/core/logger";
import { markWhatsNewSeen, unreadReleases, visibleReleases } from "@multica/core/whats-new";
import { WHATS_NEW_RELEASES, type WhatsNewRelease } from "./releases";

const logger = createLogger("whats-new");

export function useWhatsNew(releases: readonly WhatsNewRelease[] = WHATS_NEW_RELEASES) {
  const user = useAuthStore((s) => s.user);
  const serverVersion = useConfigStore((s) => s.serverVersion);

  const visible = useMemo(
    () => visibleReleases(releases.filter((r) => r.highlights.length > 0), serverVersion),
    [releases, serverVersion],
  );
  const unread = useMemo(() => unreadReleases(visible, user), [visible, user]);

  const newest = visible[0]?.version;
  const hasUnread = unread.length > 0;
  const markSeen = useCallback(() => {
    if (!newest || !hasUnread) return;
    markWhatsNewSeen(newest).catch((err) => {
      logger.warn("failed to persist seen version", err);
    });
  }, [newest, hasUnread]);

  return { visible, unread, markSeen };
}
