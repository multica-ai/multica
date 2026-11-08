"use client";

import { useCallback, useState, type RefObject } from "react";
import { Popover, PopoverContent } from "@multica/ui/components/ui/popover";
import { useWhatsNew } from "./use-whats-new";
import { WhatsNewPanel } from "./whats-new-panel";

const EMPTY_VERSIONS: ReadonlySet<string> = new Set();

/**
 * The release history behind the Help menu's What's new entry. Opening it
 * records the newest release as seen; releases the user hadn't seen yet are
 * captured first so the panel can still mark them as new.
 */
export function useWhatsNewMenuPanel() {
  const { visible, unread, markSeen } = useWhatsNew();
  const [open, setOpen] = useState(false);
  const [unreadVersions, setUnreadVersions] = useState(EMPTY_VERSIONS);
  const show = useCallback(() => {
    setUnreadVersions(new Set(unread.map((r) => r.version)));
    setOpen(true);
    markSeen();
  }, [unread, markSeen]);
  return {
    available: visible.length > 0,
    /** The newest release the user hasn't seen, for the menu entry's tag. */
    newestUnread: unread[0]?.version,
    releases: visible,
    open,
    setOpen,
    unreadVersions,
    show,
  };
}

export function WhatsNewMenuPanel({
  panel,
  anchor,
}: {
  panel: ReturnType<typeof useWhatsNewMenuPanel>;
  /** The menu's trigger: the menu itself is gone by the time the panel opens. */
  anchor: RefObject<HTMLElement | null>;
}) {
  return (
    <Popover open={panel.open} onOpenChange={panel.setOpen}>
      <PopoverContent
        anchor={anchor}
        side="right"
        align="end"
        sideOffset={8}
        className="w-[400px] gap-0 overflow-hidden rounded-xl p-0"
      >
        <WhatsNewPanel releases={panel.releases} unreadVersions={panel.unreadVersions} onClose={() => panel.setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}
