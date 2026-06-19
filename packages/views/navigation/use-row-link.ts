"use client";

import { useCallback } from "react";
import { useNavigation } from "./context";

/**
 * Whole-row click navigation for list rows.
 *
 * Call once at the top of a list component; it returns a factory that builds
 * the props to spread onto each `<ListGridRow>` (a plain `<div>`, never an
 * `<a>`) given that row's href. Calling it per row keeps it outside the
 * rules-of-hooks trap of invoking `useNavigation` inside a `.map()`.
 *
 * The whole row navigates on click — the row is the click target, so the
 * name cell stays plain text (no nested `<a>`, which would be a redundant
 * second entry point). Interactive cells (checkbox, kebab, inline editors)
 * call `stopPropagation` so clicking them never reaches these handlers.
 *
 * Mirrors AppLink's modifier semantics: a plain left click pushes; cmd/ctrl
 * (or a middle click) opens a background tab on desktop.
 *
 * Callers add `cursor-pointer` to the row's own className (kept out of the
 * returned props so it can't clash with the row's existing className).
 */
export function useRowLink() {
  const { push, openInNewTab, prefetch } = useNavigation();

  return useCallback(
    (href: string) => {
      const open = (newTab: boolean) => {
        if (newTab && openInNewTab) openInNewTab(href);
        else push(href);
      };
      return {
        onClick: (e: React.MouseEvent) => {
          // A child control already handled this click (controls call
          // stopPropagation and never reach here; defaultPrevented guards
          // any that preventDefault instead).
          if (e.defaultPrevented || e.button !== 0) return;
          open(e.metaKey || e.ctrlKey);
        },
        onAuxClick: (e: React.MouseEvent) => {
          if (e.defaultPrevented || e.button !== 1) return; // middle click
          e.preventDefault();
          open(true);
        },
        onMouseEnter: () => prefetch?.(href),
      };
    },
    [push, openInNewTab, prefetch],
  );
}
