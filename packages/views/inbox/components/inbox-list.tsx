"use client";

import { useCallback, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import { Archive, ChevronRight, Inbox } from "lucide-react";
import { isEditableShortcutTarget } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import type { InboxItem } from "@multica/core/types";
import type { InboxView } from "./inbox-view";
import { InboxListItem } from "./inbox-list-item";
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { useRestoredScrollOffset, useRestoredScrollRef } from "../../platform";
import { useT } from "../../i18n";

// Sizing only (like the board's card estimate): the seed's trailing spacer
// and Virtuoso's defaultItemHeight share this value so the scroller's height
// is truthful from the first frame and a restored offset sticks. A row is
// two text lines (body + caption) plus py-2.5.
const INBOX_ROW_ESTIMATED_HEIGHT = 58;

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
 * tab order. Arrow-key navigation (below) covers off-screen rows, because it
 * walks the data rather than the DOM.
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
  // Pull-based scroll restoration (MUL-4741): assign the saved offset at
  // ref-attach (the seed's estimate spacer gives the container a truthful
  // height on the first commit, so the assignment sticks pre-paint) and feed
  // the same offset into the Virtuoso as its initial position.
  const restoredScrollTop = useRestoredScrollOffset("list");
  const restoreScrollRef = useRestoredScrollRef("list");
  const attachScrollEl = useCallback(
    (el: HTMLDivElement | null) => {
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [restoreScrollRef],
  );
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const isArchivedView = view === "archived";

  // Keyboard focus for the list lives on the scroll container, not on a row:
  // virtualization unmounts the row the user clicked as soon as it scrolls
  // out, and focus falling back to <body> would silently stop the next arrow
  // key from reaching this handler. `preventScroll` because focusing a box
  // scrolls every scrollable ancestor to reveal it, which on desktop shoves
  // the shell around (#3929).
  const focusList = useCallback(() => {
    scrollEl?.focus({ preventScroll: true });
  }, [scrollEl]);

  const selectItem = useCallback(
    (item: InboxItem) => {
      // Safari does not focus a clicked row control, so the container has to
      // be focused explicitly or the arrow keys would stay dead after a click.
      focusList();
      onSelect(item);
    },
    [focusList, onSelect],
  );

  // Arrow keys move the selection instead of scrolling the container — what
  // every mail-style list does (MUL-5622). Bound to the scroll container
  // rather than the document so it only fires while focus is inside the list:
  // pressing Down while reading the issue detail must not swap the row out
  // from under the reader.
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    // A menu, editor, or text field that already acted on the arrow key owns
    // it; so does an IME candidate list mid-composition.
    if (event.defaultPrevented || isImeComposing(event)) return;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    if (isEditableShortcutTarget(event.target)) return;

    // Claim the key even at the ends of the list: falling through to the
    // native scroll there would move the viewport away from the selected row.
    event.preventDefault();
    focusList();

    const current = items.findIndex(
      (item) => (item.issue_id ?? item.id) === selectedKey,
    );
    const step = event.key === "ArrowDown" ? 1 : -1;
    // Nothing selected yet: Down enters the list at the top, Up at the bottom.
    const nextIndex =
      current < 0
        ? step === 1
          ? 0
          : items.length - 1
        : Math.min(Math.max(current + step, 0), items.length - 1);
    if (nextIndex === current) return;
    const nextItem = items[nextIndex];
    if (!nextItem) return;

    // Virtuoso's own scrollIntoView, never the DOM element's: the target row
    // may not be mounted, and the native call scrolls ancestors too. It is a
    // no-op while the row is already fully visible, so a selection moving
    // inside the viewport does not scroll the list.
    virtuosoRef.current?.scrollIntoView({ index: nextIndex });
    onSelect(nextItem);
  };

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
          className="mt-1 flex h-10 w-full items-center gap-2 rounded-md px-2 text-left text-caption text-muted-foreground outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        >
          <span className="flex size-7 shrink-0 items-center justify-center">
            <Archive className="size-4" />
          </span>
          <span className="min-w-0 flex-1 truncate font-medium">
            {t(($) => $.list.archived_title)}
          </span>
          <span className="shrink-0 tabular-nums text-muted-foreground">
            {archivedCount}
          </span>
          <ChevronRight className="size-4 shrink-0 text-faint-foreground" />
        </button>
      ) : null,
    [isArchivedView, archivedCount, onOpenArchived, t],
  );

  const Footer = useCallback(() => archivedEntry, [archivedEntry]);

  if (items.length === 0) {
    return (
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-faint-foreground" />
          <p className="text-body">
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
      onClick={() => selectItem(item)}
      onAction={() => onAction(item.id)}
    />
  );

  // While the callback ref hasn't handed the scroll element over yet (the first
  // render after a remount), seed a bounded slice of real rows so the list
  // never paints blank; once it's set, mount the Virtuoso with a matching
  // `initialItemCount` so the measurement frame keeps those rows (MUL-4750).
  return (
    <div
      ref={attachScrollEl}
      data-tab-scroll-root="list"
      // Programmatically focusable only: the rows already carry their own tab
      // stops, so a tabbable container would just add a redundant one.
      tabIndex={-1}
      onKeyDown={handleKeyDown}
      className="flex-1 min-h-0 overflow-y-auto outline-none"
    >
      <div className="px-2 py-1">
        {scrollEl ? (
          <Virtuoso
            ref={virtuosoRef}
            customScrollParent={scrollEl}
            data={items}
            computeItemKey={computeItemKey}
            initialScrollTop={restoredScrollTop}
            initialItemCount={Math.min(items.length, VIRTUOSO_SEED_COUNT)}
            defaultItemHeight={INBOX_ROW_ESTIMATED_HEIGHT}
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
              estimatedItemHeight={INBOX_ROW_ESTIMATED_HEIGHT}
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
