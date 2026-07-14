"use client";

import { useState } from "react";
import { Virtuoso } from "react-virtuoso";
import { Inbox } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { InboxListItem } from "./inbox-list-item";
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
  selectedKey,
  onSelect,
  onArchive,
}: {
  items: InboxItem[];
  selectedKey: string;
  onSelect: (item: InboxItem) => void;
  onArchive: (id: string) => void;
}) {
  const { t } = useT("inbox");
  // Virtuoso's `customScrollParent` wants the actual HTMLElement, not a ref.
  // A callback ref into state hands the element over once it mounts and
  // triggers the re-render that lets Virtuoso attach to it.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);

  if (items.length === 0) {
    return (
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm">{t(($) => $.list.empty)}</p>
        </div>
      </div>
    );
  }

  return (
    <div ref={setScrollEl} className="flex-1 min-h-0 overflow-y-auto">
      <div className="px-2 py-1">
        {scrollEl && (
          <Virtuoso
            customScrollParent={scrollEl}
            data={items}
            computeItemKey={(_index, item) => item.id}
            increaseViewportBy={{ top: 400, bottom: 400 }}
            itemContent={(_index, item) => (
              <InboxListItem
                item={item}
                isSelected={(item.issue_id ?? item.id) === selectedKey}
                onClick={() => onSelect(item)}
                onArchive={() => onArchive(item.id)}
              />
            )}
          />
        )}
      </div>
    </div>
  );
}
