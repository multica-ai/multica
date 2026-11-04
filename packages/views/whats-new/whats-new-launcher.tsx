"use client";

import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import { Megaphone } from "@multica/ui/icons";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { RAIL_ICON_BUTTON_CLASS } from "../layout/rail-styles";
import type { WhatsNewRelease } from "./releases";
import { useWhatsNewSessionStore } from "./session-store";
import { useWhatsNew } from "./use-whats-new";
import { WhatsNewCard } from "./whats-new-card";
import { WhatsNewPanel } from "./whats-new-panel";

/** Lets the workspace settle before the card slides in. */
export const AUTO_OPEN_DELAY_MS = 1200;

const POPUP_CLASS = "gap-0 overflow-hidden rounded-xl p-0";
const EMPTY_VERSIONS: ReadonlySet<string> = new Set();

type View =
  | { kind: "card"; releases: WhatsNewRelease[] }
  | { kind: "panel"; unreadVersions: ReadonlySet<string> };

/** A rail can be mounted but hidden (the md:flex rail on narrow screens). */
function isRendered(el: HTMLElement | null): boolean {
  if (!el) return false;
  return typeof el.checkVisibility === "function" ? el.checkVisibility() : true;
}

/**
 * Rail entry for unread release highlights. After an update the card opens by
 * itself once per page session, from whichever launcher is on screen;
 * dismissing it (or opening the panel) records the
 * newest release as seen. Clicking away only hides the card, so the dot
 * stays until the user acknowledges it. Once nothing is unread the button
 * leaves the rail; the history stays one click away in the Help menu.
 */
export function WhatsNewLauncher() {
  const { t } = useT("layout");
  const { visible, unread, markSeen } = useWhatsNew();
  const [open, setOpen] = useState(false);
  // Keeps the trigger in the rail while its popup animates out: marking seen
  // empties `unread` while the popup is still visible.
  const [popupMounted, setPopupMounted] = useState(false);
  // Snapshotted on open, for the same reason.
  const [view, setView] = useState<View>({ kind: "panel", unreadVersions: EMPTY_VERSIONS });
  const triggerRef = useRef<HTMLButtonElement>(null);
  const hasUnread = unread.length > 0;

  useEffect(() => {
    if (unread.length === 0 || useWhatsNewSessionStore.getState().autoShown) return;
    const timer = window.setTimeout(() => {
      const session = useWhatsNewSessionStore.getState();
      if (session.autoShown || !isRendered(triggerRef.current)) return;
      session.markAutoShown();
      setView({ kind: "card", releases: unread });
      setPopupMounted(true);
      setOpen(true);
    }, AUTO_OPEN_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [unread]);

  if (visible.length === 0 || (!hasUnread && !popupMounted)) return null;

  const openPanel = () => {
    setView({ kind: "panel", unreadVersions: new Set(unread.map((r) => r.version)) });
    setPopupMounted(true);
    setOpen(true);
    markSeen();
  };
  const dismiss = () => {
    setOpen(false);
    markSeen();
  };
  const label = t(($) => $.whats_new.trigger);

  return (
    <Popover
      open={open}
      onOpenChange={(next, details) => {
        // Pressing the rail button while the card is up means "show me
        // everything", not "close".
        if (next || (view.kind === "card" && details.reason === "trigger-press")) openPanel();
        else setOpen(false);
      }}
      onOpenChangeComplete={(next) => {
        if (!next) setPopupMounted(false);
      }}
    >
      <TooltipHint label={label} side="right">
        <PopoverTrigger
          ref={triggerRef}
          aria-label={label}
          data-unread={hasUnread || undefined}
          className={RAIL_ICON_BUTTON_CLASS}
        >
          <Megaphone />
          {hasUnread && (
            <span
              aria-hidden
              data-slot="whats-new-dot"
              className="absolute top-1.5 right-1.5 size-2 rounded-full bg-brand ring-2 ring-app-shell"
            />
          )}
        </PopoverTrigger>
      </TooltipHint>
      <PopoverContent
        side="right"
        align="end"
        sideOffset={8}
        // An unprompted card must not pull focus out of whatever the user is typing in.
        initialFocus={view.kind === "panel"}
        className={cn(POPUP_CLASS, view.kind === "card" ? "w-[380px]" : "w-[400px]")}
      >
        {view.kind === "card" ? (
          <WhatsNewCard releases={view.releases} onDismiss={dismiss} onViewDetails={openPanel} />
        ) : (
          <WhatsNewPanel releases={visible} unreadVersions={view.unreadVersions} onClose={() => setOpen(false)} />
        )}
      </PopoverContent>
    </Popover>
  );
}

/** The release history opened from a menu rather than the rail button. */
export function useWhatsNewMenuPanel() {
  const { visible, unread, markSeen } = useWhatsNew();
  const [open, setOpen] = useState(false);
  const [unreadVersions, setUnreadVersions] = useState(EMPTY_VERSIONS);
  const show = useCallback(() => {
    setUnreadVersions(new Set(unread.map((r) => r.version)));
    setOpen(true);
    markSeen();
  }, [unread, markSeen]);
  return { available: visible.length > 0, releases: visible, open, setOpen, unreadVersions, show };
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
      <PopoverContent anchor={anchor} side="right" align="end" sideOffset={8} className={cn(POPUP_CLASS, "w-[400px]")}>
        <WhatsNewPanel releases={panel.releases} unreadVersions={panel.unreadVersions} onClose={() => panel.setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}
