"use client";

import type { KeyboardEvent } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { InboxTab } from "./inbox-view";

const TABS: readonly InboxTab[] = ["inbox", "snoozed", "archived"];

/**
 * The Inbox's three lists as tabs: the live Inbox with its unread count, the
 * issues snoozed out of it with theirs, and the archive. The Work panel's
 * Inbox section shows them in its header; a phone's Inbox page, in its own.
 */
export function InboxTabs({
  tab,
  onSelect,
  unread,
  snoozed,
  size = "section",
  className,
}: {
  tab: InboxTab;
  onSelect: (tab: InboxTab) => void;
  /** Left out (or 0) to show no count. */
  unread?: number;
  snoozed?: number;
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
        return (
          <button
            key={id}
            type="button"
            role="tab"
            data-inbox-tab={id}
            aria-selected={active}
            tabIndex={active ? 0 : -1}
            onClick={() => onSelect(id)}
            className={cn(
              // Long labels (French, Russian) truncate rather than push the
              // section's actions out of the header.
              "flex min-w-0 items-center gap-1 rounded-md outline-none transition-colors focus-visible:outline-2 focus-visible:outline-ring",
              size === "section" ? "h-5 px-1.5" : "h-7 px-2",
              active ? "bg-surface-selected text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            <span className="truncate">{labels[id]}</span>
            {count > 0 && (
              <span className={cn("shrink-0 tabular-nums", id === "inbox" ? "text-brand" : "text-faint-foreground")}>
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
