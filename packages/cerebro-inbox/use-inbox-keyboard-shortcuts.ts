"use client";

import { useEffect } from "react";

interface Args {
  /** Item id of the currently-selected inbox row. */
  selectedId: string | null;
  /** Archive the supplied item id. */
  onArchive: (id: string) => void;
}

/**
 * Wires the cerebro keyboard shortcut for the inbox page:
 *
 *  - `e` — archive the currently-selected row (Linear / Gmail style)
 *
 * Mount once at the inbox page level; the hook is a no-op when no row is
 * selected. Triggers only when no input/textarea is focused so users typing
 * in the search box don't accidentally archive things.
 */
export function useInboxKeyboardShortcuts({ selectedId, onArchive }: Args) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!selectedId) return;
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName?.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;
      if (e.key === "e") {
        e.preventDefault();
        onArchive(selectedId);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [selectedId, onArchive]);
}
