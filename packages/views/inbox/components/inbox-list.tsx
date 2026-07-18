"use client";

import { useCallback, useMemo, useState } from "react";
import { Virtuoso } from "react-virtuoso";
import { Archive, ChevronRight, Inbox } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import type { InboxView } from "./inbox-view";
import { InboxListItem } from "./inbox-list-item";
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { useT } from "../../i18n";

/**
 * Scrollable, virtualized inbox notification list.
 *
 * Owns the scroll container so both the mobile and desktop layouts render an
 * identical scroller. Rows are virtualized via react-virtuoso so only the
 * visible window (plus a small overscan) is mounted — the notification list
 * can grow long and every row otherwise carries an avatar + hover card, so
 * mounting all of them inflates the tab-switch commit (MUL-4474).
 *
 * Virtualization changes exactly one thing: whether an off-screen row is in
 * the DOM. Selection, hover, archive, and scroll semantics are unchanged —
 * the row component and the callbacks are the same as the non-virtualized
 * list. `customScrollParent` keeps Virtuoso reading/writing the existing
 * `overflow-y-auto` element (same pattern as the issue-detail timeline), so
 * scroll position behaves exactly as before.
 *
 * Known virtualization tradeoff: keyboard Tab only reaches currently-mounted
 * rows; a keyboard-only user must scroll to bring off-screen rows into the
 * tab order. The inbox has no custom arrow-key list navigation, so the
 * practical surface is small, but it is called out for the manual pass.
 */
export function InboxList({
  items,
  view,
  selectedKey,
  archivedCount,
  onSelect,
  onAction,
  onOpenArchived,
}: {
  items: InboxItem[];
  view: InboxView;
  selectedKey: string;
  // Deduplicated archived-issue count. Only read in the main view, to label the
  // entry into the archive; the entry hides at zero.
  archivedCount: number;
  onSelect: (item: InboxItem) => void;
  onAction: (id: string) => void;
  onOpenArchived: () => void;
}) {
  const { t } = useT("inbox");
  // Virtuoso's `customScrollParent` wants the actual HTMLElement, not a ref.
  // A callback ref into state hands the element over once it mounts and
  // triggers the re-render that lets Virtuoso attach to it.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const isArchivedView = view === "archived";

  // The entry into the archive sits below the last row and scrolls with the
  // list (same placement as chat's). Virtuoso mounts it via `components.Footer`,
  // and swaps the component whenever that prop's identity changes — so both the
  // element and the Footer wrapping it are memoized. Without that the entry
  // remounts on every parent render and drops hover/focus mid-click.
  const archivedEntry = useMemo(
    () =>
      !isArchivedView && archivedCount > 0 ? (
        <button
          type="button"
          onClick={onOpenArchived}
          className="mt-1 flex h-10 w-full items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          <span className="flex size-7 shrink-0 items-center justify-center">
            <Archive className="size-4" />
          </span>
          <span className="min-w-0 flex-1 truncate font-medium">
            {t(($) => $.list.archived_title)}
          </span>
          <span className="shrink-0 tabular-nums text-muted-foreground/70">
            {archivedCount}
          </span>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground/50" />
        </button>
      ) : null,
    [isArchivedView, archivedCount, onOpenArchived, t],
  );

  const Footer = useCallback(() => archivedEntry, [archivedEntry]);

  if (items.length === 0) {
    return (
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm">
            {isArchivedView
              ? t(($) => $.list.archived_empty)
              : t(($) => $.list.empty)}
          </p>
        </div>
        {/* Still offer the archive when the main list is empty — that is
            exactly when a user goes looking for what they filed away. */}
        {archivedEntry && <div className="px-2">{archivedEntry}</div>}
      </div>
    );
  }

  const computeItemKey = (_index: number, item: InboxItem) => item.id;
  const itemContent = (_index: number, item: InboxItem) => (
    <InboxListItem
      item={item}
      view={view}
      isSelected={(item.issue_id ?? item.id) === selectedKey}
      onClick={() => onSelect(item)}
      onAction={() => onAction(item.id)}
    />
  );

  // While the callback ref hasn't handed the scroll element over yet (the first
  // render after a remount), seed a bounded slice of real rows so the list
  // never paints blank; once it's set, mount the Virtuoso with a matching
  // `initialItemCount` so the measurement frame keeps those rows (MUL-4750).
  return (
    <div ref={setScrollEl} className="flex-1 min-h-0 overflow-y-auto">
      <div className="px-2 py-1">
        {scrollEl ? (
          <Virtuoso
            customScrollParent={scrollEl}
            data={items}
            computeItemKey={computeItemKey}
            initialItemCount={Math.min(items.length, VIRTUOSO_SEED_COUNT)}
            increaseViewportBy={{ top: 400, bottom: 400 }}
            itemContent={itemContent}
            components={{ Footer }}
          />
        ) : (
          <>
            <VirtuosoSeed
              data={items}
              itemContent={itemContent}
              computeItemKey={computeItemKey}
            />
            {/* The seed frame renders a bounded slice, so the entry would be
                mid-list rather than after the last row — only show it once the
                seed IS the whole list. */}
            {items.length <= VIRTUOSO_SEED_COUNT && archivedEntry}
          </>
        )}
      </div>
    </div>
  );
}
