"use client";

import { useEffect, useCallback, useRef } from "react";
import { useT } from "../../i18n";

/** Hook returning a localized relative-time formatter. */
export function useTimeAgo() {
  const { t } = useT("inbox");
  return (dateStr: string): string => {
    const diff = Date.now() - new Date(dateStr).getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return t(($) => $.list.time.just_now);
    if (minutes < 60) return t(($) => $.list.time.minutes, { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t(($) => $.list.time.hours, { count: hours });
    const days = Math.floor(hours / 24);
    return t(($) => $.list.time.days, { count: days });
  };
}

export interface KeyboardNavState {
  focusedIndex: number;
  totalItems: number;
}

/**
 * Hook for inbox keyboard navigation.
 * - j / ArrowDown: move down
 * - k / ArrowUp: move up
 * - e: archive focused item
 * - /: focus search input
 * - Escape: clear search or exit multiselect mode
 */
export function useInboxKeyboardNav({
  itemCount,
  enabled,
  onSelectIndex,
  onArchiveFocused,
  onFocusSearch,
  onEscape,
}: {
  itemCount: number;
  enabled: boolean;
  onSelectIndex: (index: number) => void;
  onArchiveFocused: (index: number) => void;
  onFocusSearch: () => void;
  onEscape: () => void;
}) {
  const focusedIndexRef = useRef(-1);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!enabled) return;

      // Don't capture when typing in inputs
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") {
        if (e.key === "Escape") {
          (e.target as HTMLElement).blur();
          onEscape();
        }
        return;
      }

      switch (e.key) {
        case "j":
        case "ArrowDown": {
          e.preventDefault();
          const next = Math.min(focusedIndexRef.current + 1, itemCount - 1);
          focusedIndexRef.current = next;
          onSelectIndex(next);
          break;
        }
        case "k":
        case "ArrowUp": {
          e.preventDefault();
          const prev = Math.max(focusedIndexRef.current - 1, 0);
          focusedIndexRef.current = prev;
          onSelectIndex(prev);
          break;
        }
        case "e": {
          e.preventDefault();
          if (focusedIndexRef.current >= 0) {
            onArchiveFocused(focusedIndexRef.current);
          }
          break;
        }
        case "/": {
          e.preventDefault();
          onFocusSearch();
          break;
        }
        case "Escape": {
          e.preventDefault();
          onEscape();
          break;
        }
      }
    },
    [enabled, itemCount, onSelectIndex, onArchiveFocused, onFocusSearch, onEscape],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  return {
    resetFocus: () => {
      focusedIndexRef.current = -1;
    },
    setFocusIndex: (index: number) => {
      focusedIndexRef.current = index;
    },
  };
}
