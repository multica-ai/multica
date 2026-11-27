"use client";

import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useCommentComposerStore } from "@multica/core/issues/stores";

/**
 * Whether the bottom comment composer is pinned to the scroll viewport.
 *
 * The pin preference (Settings → Preferences) applies as-is from `md` up.
 * Below `md` it also needs the separate small-screen opt-in, which is off by
 * default: pinned on a phone, the composer costs a lasting share of a surface
 * people mostly read on, so the default keeps it at the end of the timeline.
 * When pinned there, the issue page keeps the composer clear of the chat
 * launcher's corner (see `--chat-launcher-clearance`).
 *
 * The preferences are only read here, never rewritten, so a viewport that
 * crosses `md` follows them without changing what the user chose.
 *
 * Both the composer and its host read this, so the pinning and the 40vh height
 * cap that only makes sense while pinned can never disagree.
 */
export function useStickyComposer(): boolean {
  const preferred = useCommentComposerStore((s) => s.sticky);
  const preferredOnSmallScreens = useCommentComposerStore((s) => s.stickyOnSmallScreens);
  const isMobile = useIsMobile();
  return preferred && (!isMobile || preferredOnSmallScreens);
}
