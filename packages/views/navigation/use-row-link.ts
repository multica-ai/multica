"use client";

import { useCallback } from "react";
import { resolveClickIntent, type LinkClickIntent } from "./click-intent";
import { useNavigation } from "./context";

/**
 * Whole-row navigation uses a div, so useRowLink resolves modifier and middle
 * clicks explicitly through the desktop adapter or a shareable browser URL.
 * Call the hook once per list and use its returned row-props factory.
 *
 * Nested interactive cells use rowLinkInteractiveProps to stop both click and
 * auxclick propagation. Do not prevent their defaults: anchors must retain native
 * middle-click behavior. Callers own cursor styling and temporary tab titles.
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
