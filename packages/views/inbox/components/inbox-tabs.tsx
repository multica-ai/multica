"use client";

import type { KeyboardEvent } from "react";
import { AlarmClock, Archive, Inbox } from "@multica/ui/icons";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { InboxTab } from "./inbox-view";

const TABS: readonly InboxTab[] = ["inbox", "snoozed", "archived"];

const TAB_ICONS: Record<InboxTab, typeof Inbox> = {
  inbox: Inbox,
  snoozed: AlarmClock,
  archived: Archive,
};

/**
 * The Inbox's three lists as tabs: the live Inbox with its unread count, the
 * issues snoozed out of it with theirs, and the archive. The Work panel's
 * Inbox section shows them in its header; a phone's Inbox page, in its own.
 *
 * In the section header only the open tab carries its name: the other two are
 * icons named by a tooltip, so at the panel's default width no label is cut
 * down to its first letter. The unread count stays on the Inbox
 * icon; the snoozed count moves into its tooltip.
 */
export function InboxTabs({
  tab,
  onSelect,
  unread,
  snoozed,
  expanded,
  size = "section",
  className,
}: {
  tab: InboxTab;
  onSelect: (tab: InboxTab) => void;
  /** Left out (or 0) to show no count. */
  unread?: number;
  snoozed?: number;
  /** Set when selecting the open tab again collapses the list it heads. */
  expanded?: boolean;
  /** `section` for the Work panel's header, `page` for a page header. */
  size?: "section" | "page";
  className?: string;
}) {
  const { t } = useT("inbox");
  const labels: Record<InboxTab, string> = {
    inbox: t(($) => $.page.title),
    snoozed: t(($) => $.list.snoozed_title),
    archived: t(($) => $.list.archived_title),
  };
  const counts: Partial<Record<InboxTab, number>> = { inbox: unread, snoozed };
  // Arrow keys move between the tabs, as a tab list's do.
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const at = TABS.indexOf(tab) + (event.key === "ArrowRight" ? 1 : -1);
    const next = TABS[(at + TABS.length) % TABS.length]!;
    onSelect(next);
    event.currentTarget.querySelector<HTMLElement>(`[data-inbox-tab="${next}"]`)?.focus();
  };

  return (
    <div
      role="tablist"
      aria-label={labels.inbox}
      onKeyDown={onKeyDown}
      className={cn("flex min-w-0 items-center gap-0.5", size === "section" ? "-ml-1.5" : "-ml-2", className)}
    >
      {TABS.map((id) => {
        const active = id === tab;
        const count = counts[id] ?? 0;
        const iconOnly = size === "section" && !active;
        const countClass = cn("shrink-0 tabular-nums", id === "inbox" ? "text-brand" : "text-faint-foreground");
        const Icon = TAB_ICONS[id];
        const button = (
          <button
            type="button"
            role="tab"
            data-inbox-tab={id}
            aria-selected={active}
            aria-expanded={active ? expanded : undefined}
            tabIndex={active ? 0 : -1}
            onClick={() => onSelect(id)}
            className={cn(
              "flex items-center gap-1 rounded-md outline-none transition-colors focus-visible:outline-2 focus-visible:outline-ring",
              size === "section" ? "h-5" : "h-7 px-2",
              // The open tab gives up width before the icons do; a label
              // still too long for the panel (Spanish, French) truncates.
              iconOnly ? "shrink-0 px-1" : size === "section" ? "min-w-0 px-1.5" : "min-w-0",
              active ? "bg-surface-selected text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {iconOnly ? (
              <>
                <Icon aria-hidden className="size-3.5" />
                <span className="sr-only">{labels[id]}</span>
                {count > 0 && <span className={cn(countClass, id !== "inbox" && "sr-only")}>{count}</span>}
              </>
            ) : (
              <>
                <span className="truncate">{labels[id]}</span>
                {count > 0 && <span className={countClass}>{count}</span>}
              </>
            )}
          </button>
        );
        // Every tab keeps the same element as it opens and closes, so arrow
        // keys keep focus on it; only the icons' tooltips are enabled.
        return (
          <Tooltip key={id} disabled={!iconOnly}>
            <TooltipTrigger render={button} />
            <TooltipContent side="bottom">{count > 0 ? `${labels[id]} · ${count}` : labels[id]}</TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}
