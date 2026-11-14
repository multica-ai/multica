"use client";

import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import {
  useInboxSectionStore,
  type InboxFoldingSection,
} from "@multica/core/inbox/section-store";
import type {
  InboxSectionRow,
  InboxSections,
  InboxWorkingRow,
} from "@multica/core/inbox/sections";
import { isEditableShortcutTarget } from "@multica/core/shortcuts";
import type { InboxItem } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import { CheckCheck, ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { useT } from "../../i18n";
import { useRestoredScrollOffset, useRestoredScrollRef } from "../../platform";
import { InboxListItem, type InboxRowSection } from "./inbox-list-item";
import { InboxWorkingItem } from "./inbox-working-item";

// Sizing only, shared by the seed's spacer and Virtuoso's default: a
// "Needs you" row is two text lines plus py-2.5; the other rows are shorter
// and get measured once mounted.
const INBOX_ROW_ESTIMATED_HEIGHT = 58;

type SectionKey = "needs_you" | "working" | InboxFoldingSection;

type Entry =
  | { kind: "header"; section: SectionKey; count: number | null; folding: boolean; open: boolean }
  | { kind: "item"; section: InboxRowSection; row: InboxSectionRow }
  | { kind: "working"; row: InboxWorkingRow }
  | { kind: "caught-up"; agents: number };

/** The key the page selects a row by. */
function entryKey(entry: Entry): string | null {
  if (entry.kind === "item") return entry.row.item.issue_id ?? entry.row.item.id;
  if (entry.kind === "working") return entry.row.issueId;
  return null;
}

/**
 * The main Inbox list: one column, sectioned by whose turn it is.
 *
 * "Needs you" leads and is always there, so an empty one says so instead of
 * leaving the column to look broken. "Working" follows, then "Updates" and
 * "Snoozed", which start folded: nothing in them is waiting.
 *
 * "Updates" holds what has not been read yet. Opening one reads it and it
 * leaves for the archive; "Clear" sends the rest there unread.
 *
 * The sections are one virtualized list rather than four, so the column has a
 * single scroller and the arrow keys walk straight through. Under a filter
 * the list is the matches: folded sections open and empty ones are left out.
 */
export function InboxSectionedList({
  sections,
  selectedKey,
  filtered = false,
  onSelect,
  onSelectIssue,
  onAction,
  onClearUpdates,
  emptyLabel,
  emptyAction,
  scrollRestoration = true,
}: {
  sections: InboxSections;
  selectedKey: string;
  /** A filter is on: show matches only, and never the caught-up state. */
  filtered?: boolean;
  onSelect: (item: InboxItem) => void;
  /** Open an issue that has no notification: a "Working" row. */
  onSelectIssue: (issueId: string) => void;
  onAction: (id: string) => void;
  /** Archive every update listed. Absent where the list is only a preview. */
  onClearUpdates?: () => void;
  emptyLabel?: string;
  emptyAction?: ReactNode;
  /**
   * Off for a preview rendered over another route, which must neither read
   * nor be captured as that route's `list` scroll offset.
   */
  scrollRestoration?: boolean;
}) {
  const { t } = useT("inbox");
  const stored = useInboxSectionStore((s) => s.open);
  const toggle = useInboxSectionStore((s) => s.toggle);
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const savedScrollTop = useRestoredScrollOffset("list");
  const restoreScrollRef = useRestoredScrollRef("list");
  const restoredScrollTop = scrollRestoration ? savedScrollTop : undefined;
  const attachScrollEl = useCallback(
    (el: HTMLDivElement | null) => {
      setScrollEl(el);
      if (scrollRestoration) restoreScrollRef(el);
    },
    [restoreScrollRef, scrollRestoration],
  );
  const virtuosoRef = useRef<VirtuosoHandle>(null);

  const entries = useMemo(() => {
    const out: Entry[] = [];
    const { needsYou, working, updates, snoozed } = sections;
    const header = (section: SectionKey, count: number | null, open = true) =>
      out.push({ kind: "header", section, count, folding: !filtered && section !== "needs_you" && section !== "working", open });

    if (needsYou.length > 0 || !filtered) {
      header("needs_you", needsYou.length > 0 ? needsYou.length : null);
      for (const row of needsYou) out.push({ kind: "item", section: "needs_you", row });
      if (needsYou.length === 0) {
        out.push({ kind: "caught-up", agents: new Set(working.map((row) => row.agentId)).size });
      }
    }
    if (working.length > 0) {
      header("working", working.length);
      for (const row of working) out.push({ kind: "working", row });
    }
    if (updates.length > 0) {
      const open = filtered || stored.updates;
      header("updates", updates.length, open);
      if (open) for (const row of updates) out.push({ kind: "item", section: "updates", row });
    }
    if (snoozed.length > 0) {
      const open = filtered || stored.snoozed;
      header("snoozed", snoozed.length, open);
      if (open) for (const row of snoozed) out.push({ kind: "item", section: "snoozed", row });
    }
    return out;
  }, [sections, filtered, stored]);

  // Keyboard focus lives on the scroll container, not on a row: a virtualized
  // row unmounts as it scrolls out and would take the focus with it.
  const focusList = useCallback(() => {
    scrollEl?.focus({ preventScroll: true });
  }, [scrollEl]);

  const selectEntry = useCallback(
    (entry: Entry) => {
      focusList();
      if (entry.kind === "item") onSelect(entry.row.item);
      else if (entry.kind === "working") onSelectIssue(entry.row.issueId);
    },
    [focusList, onSelect, onSelectIssue],
  );

  // Arrow keys move the selection through every section's rows, skipping the
  // headers. Bound to the container so they only act while focus is in the
  // list, and never swap the row out from under someone reading the detail.
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    if (event.defaultPrevented || isImeComposing(event)) return;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    if (isEditableShortcutTarget(event.target)) return;

    event.preventDefault();
    focusList();

    const rows: number[] = [];
    entries.forEach((entry, index) => {
      if (entryKey(entry) != null) rows.push(index);
    });
    if (rows.length === 0) return;
    const current = rows.findIndex((index) => entryKey(entries[index]!) === selectedKey);
    const step = event.key === "ArrowDown" ? 1 : -1;
    const next =
      current < 0
        ? step === 1
          ? 0
          : rows.length - 1
        : Math.min(Math.max(current + step, 0), rows.length - 1);
    if (next === current) return;
    const index = rows[next]!;
    virtuosoRef.current?.scrollIntoView({ index });
    selectEntry(entries[index]!);
  };

  const sectionLabel: Record<SectionKey, string> = {
    needs_you: t(($) => $.sections.needs_you),
    working: t(($) => $.sections.working),
    updates: t(($) => $.sections.updates),
    snoozed: t(($) => $.sections.snoozed),
  };

  if (entries.length === 0) {
    return (
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="px-5 pt-2.5 pb-1.5 text-caption text-muted-foreground">
          <p>{emptyLabel ?? t(($) => $.list.empty)}</p>
          {emptyAction && <div className="mt-3">{emptyAction}</div>}
        </div>
      </div>
    );
  }

  const computeItemKey = (_index: number, entry: Entry) =>
    entry.kind === "header"
      ? `header:${entry.section}`
      : entry.kind === "caught-up"
        ? "caught-up"
        : `${entry.kind}:${entryKey(entry)}`;

  const itemContent = (index: number, entry: Entry) => {
    if (entry.kind === "header") {
      const label = (
        <>
          <span className="text-foreground">{sectionLabel[entry.section]}</span>
          {entry.count != null && <span className="tabular-nums">{entry.count}</span>}
        </>
      );
      const className =
        "flex min-w-0 flex-1 items-center gap-1.5 py-1 text-caption font-medium text-muted-foreground";
      // A button cannot hold another, so "Clear" sits beside the fold toggle
      // rather than inside it.
      const clear = entry.section === "updates" && entry.open && onClearUpdates && (
        <button
          type="button"
          onClick={onClearUpdates}
          className="shrink-0 rounded-md px-1.5 py-0.5 text-caption text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          {t(($) => $.sections.clear_updates)}
        </button>
      );
      return (
        <div
          role="heading"
          aria-level={2}
          data-section-header={entry.section}
          className={cn("flex items-center gap-1 px-2", index === 0 ? "pt-0.5" : "pt-2")}
        >
          {entry.folding ? (
            <>
              <button
                type="button"
                aria-expanded={entry.open}
                onClick={() => toggle(entry.section as InboxFoldingSection)}
                className={cn(className, "rounded-md text-left outline-none focus-visible:ring-1 focus-visible:ring-ring")}
              >
                {label}
              </button>
              {clear}
              <ChevronRight
                aria-hidden
                onClick={() => toggle(entry.section as InboxFoldingSection)}
                className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", entry.open && "rotate-90")}
              />
            </>
          ) : (
            <>
              <div className={className}>{label}</div>
              {clear}
            </>
          )}
        </div>
      );
    }
    if (entry.kind === "caught-up") {
      return (
        <div className="flex flex-col items-center px-4 pt-5 pb-4 text-center">
          <CheckCheck className="h-4 w-4 text-muted-foreground" />
          <p className="mt-2 text-body font-medium">{t(($) => $.sections.caught_up)}</p>
          {entry.agents > 0 && (
            <p className="mt-1 text-caption text-muted-foreground">
              {t(($) => $.sections.still_working, { count: entry.agents })}
            </p>
          )}
        </div>
      );
    }
    if (entry.kind === "working") {
      return (
        <InboxWorkingItem
          row={entry.row}
          isSelected={entry.row.issueId === selectedKey}
          onClick={() => selectEntry(entry)}
        />
      );
    }
    const { item } = entry.row;
    return (
      <InboxListItem
        item={item}
        view="inbox"
        section={entry.section}
        row={entry.row}
        isSelected={(item.issue_id ?? item.id) === selectedKey}
        onClick={() => selectEntry(entry)}
        onAction={() => onAction(item.id)}
      />
    );
  };

  return (
    <div
      ref={attachScrollEl}
      data-tab-scroll-root={scrollRestoration ? "list" : undefined}
      tabIndex={-1}
      onKeyDown={handleKeyDown}
      className="flex-1 min-h-0 overflow-y-auto outline-none"
    >
      <div className="px-2 py-1">
        {scrollEl ? (
          <Virtuoso
            ref={virtuosoRef}
            customScrollParent={scrollEl}
            data={entries}
            computeItemKey={computeItemKey}
            initialScrollTop={restoredScrollTop}
            initialItemCount={Math.min(entries.length, VIRTUOSO_SEED_COUNT)}
            defaultItemHeight={INBOX_ROW_ESTIMATED_HEIGHT}
            increaseViewportBy={{ top: 400, bottom: 400 }}
            itemContent={itemContent}
          />
        ) : (
          <VirtuosoSeed
            data={entries}
            itemContent={itemContent}
            computeItemKey={computeItemKey}
            estimatedItemHeight={INBOX_ROW_ESTIMATED_HEIGHT}
          />
        )}
      </div>
    </div>
  );
}
