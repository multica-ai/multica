"use client";

import { useMemo, useRef } from "react";
import type { PreviewDockYield } from "../editor/preview-dock";
import { useAppShell } from "./app-shell-context";

// What the nav takes from the page: the space panel, or a list column
// (Chat). Whichever the current space shows is the one with a width.
function navWidth(): number {
  let width = 0;
  for (const element of document.querySelectorAll('[data-slot="space-panel"], [data-slot="list-column"]')) {
    width += element.getBoundingClientRect().width;
  }
  return width;
}

/**
 * The left nav as the first thing a docked preview asks to step aside. Parks
 * with the nav toggle, which is not persisted (see SidebarProvider), and only
 * restores a nav that is still collapsed: one the reader reopened with ⌘B
 * while the pane was up stays open, and closing the pane leaves it alone.
 */
export function usePreviewNavYield(): PreviewDockYield | null {
  const shell = useAppShell();
  const shellRef = useRef(shell);
  shellRef.current = shell;
  const parkedRef = useRef(false);
  const hasShell = shell !== null;

  return useMemo<PreviewDockYield | null>(
    () =>
      hasShell
        ? {
            reclaimable: () => {
              const current = shellRef.current;
              return current && !current.navCollapsed ? navWidth() : 0;
            },
            park: () => {
              const current = shellRef.current;
              if (!current || current.navCollapsed) return;
              parkedRef.current = true;
              current.toggleNav();
            },
            restore: () => {
              if (!parkedRef.current) return;
              parkedRef.current = false;
              const current = shellRef.current;
              if (current?.navCollapsed) current.toggleNav();
            },
          }
        : null,
    [hasShell],
  );
}
