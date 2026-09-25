"use client";

import { useCallback } from "react";
import { resolveClickIntent, type LinkClickIntent } from "./click-intent";
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
 * must stop propagation so interacting with them never reaches these
 * handlers — spread `rowLinkInteractiveProps` on them rather than wiring
 * `onClick` alone, because the row handles `auxclick` too.
 *
 * Mirrors AppLink's modifier semantics via `resolveClickIntent`: a plain left
 * click pushes; cmd/ctrl (or a middle click) opens a background tab on
 * desktop; cmd/ctrl+shift opens a foreground tab. On web there is no adapter
 * and — because the row is a `<div>`, not an `<a>` — no native modifier-click
 * behaviour to inherit either, so the browser tab is opened here against the
 * shareable URL (always foreground: JS cannot open a background browser tab).
 * Without that fallback a modifier or middle click would silently navigate in
 * place.
 *
 * `newTabTitle` labels the desktop tab a modifier/middle click creates until
 * the tab bar resolves the real title from the URL.
 *
 * Callers add `cursor-pointer` to the row's own className (kept out of the
 * returned props so it can't clash with the row's existing className).
 */
/**
 * Props for interactive elements nested inside a `useRowLink` row.
 *
 * Stops BOTH `click` and `auxclick` from bubbling to the row. Stopping only
 * `click` is a trap: a middle click bubbles as `auxclick`, the row handler
 * calls `preventDefault()`, and for a nested `<a>` that cancels the anchor's
 * native middle-click open — so the row opens in a background tab instead of
 * the link's own destination. Native behaviour on the element itself is
 * untouched (no `preventDefault` here), so anchors keep their default open.
 */
export const rowLinkInteractiveProps = {
  onClick: (e: React.MouseEvent) => e.stopPropagation(),
  onAuxClick: (e: React.MouseEvent) => e.stopPropagation(),
};

export function useRowLink() {
  const { push, openInNewTab, prefetch, getShareableUrl } = useNavigation();

  return useCallback(
    (href: string, newTabTitle?: string) => {
      const open = (intent: LinkClickIntent) => {
        if (intent === "push") {
          push(href);
          return;
        }
        if (openInNewTab) {
          if (intent === "foreground-tab") {
            openInNewTab(href, newTabTitle, { activate: true });
          } else {
            openInNewTab(href, newTabTitle);
          }
          return;
        }
        window.open(getShareableUrl(href), "_blank", "noopener,noreferrer");
      };
      return {
        onClick: (e: React.MouseEvent) => {
          // A child control already handled this click (controls call
          // stopPropagation and never reach here; defaultPrevented guards
          // any that preventDefault instead).
          if (e.defaultPrevented || e.button !== 0) return;
          open(resolveClickIntent(e));
        },
        onAuxClick: (e: React.MouseEvent) => {
          if (e.defaultPrevented || e.button !== 1) return; // middle click
          e.preventDefault();
          open("background-tab");
        },
        onMouseEnter: () => prefetch?.(href),
      };
    },
    [push, openInNewTab, prefetch, getShareableUrl],
  );
}
