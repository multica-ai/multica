"use client";

import { useEffect, useRef, useState } from "react";
import { Gift } from "@multica/ui/icons";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { RAIL_ICON_BUTTON_CLASS } from "../layout/rail-styles";
import type { WhatsNewRelease } from "./releases";
import { useWhatsNew } from "./use-whats-new";
import { WhatsNewCard } from "./whats-new-card";
import { WhatsNewPanel } from "./whats-new-panel";

/** Lets the workspace settle before the card slides in. */
export const AUTO_OPEN_DELAY_MS = 1200;

type View =
  | { kind: "card"; releases: WhatsNewRelease[] }
  | { kind: "panel"; unreadVersions: ReadonlySet<string> };

/**
 * Rail entry for release highlights. After an update the card opens by
 * itself once per mount; dismissing it (or opening the panel) records the
 * newest release as seen. Clicking away only hides the card, so the dot
 * stays until the user acknowledges it.
 */
export function WhatsNewLauncher() {
  const { t } = useT("layout");
  const { visible, unread, markSeen } = useWhatsNew();
  const [open, setOpen] = useState(false);
  // Snapshotted on open: marking seen empties `unread` while the popup is
  // still visible and animating out.
  const [view, setView] = useState<View>({ kind: "panel", unreadVersions: new Set() });
  const autoOpened = useRef(false);
  const hasUnread = unread.length > 0;

  useEffect(() => {
    if (unread.length === 0 || autoOpened.current) return;
    const timer = window.setTimeout(() => {
      autoOpened.current = true;
      setView({ kind: "card", releases: unread });
      setOpen(true);
    }, AUTO_OPEN_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [unread]);

  if (visible.length === 0) return null;

  const openPanel = () => {
    setView({ kind: "panel", unreadVersions: new Set(unread.map((r) => r.version)) });
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
    >
      <TooltipHint label={label} side="right">
        <PopoverTrigger aria-label={label} data-unread={hasUnread || undefined} className={RAIL_ICON_BUTTON_CLASS}>
          <Gift />
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
        className={cn("gap-0 overflow-hidden rounded-xl p-0", view.kind === "card" ? "w-[380px]" : "w-[400px]")}
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
