import type { CSSProperties } from "react";

/**
 * 48px-tall transparent strip that claims `-webkit-app-region: drag` so
 * macOS users can grab the window by its top edge — under (and making
 * room for) the native traffic lights.
 *
 * Place as the first flex child of any full-window, non-dashboard view
 * (onboarding, new-workspace, invite, no-access, etc.). The strip has
 * no background of its own; the parent's bg fills through it so the
 * page reads as "edge-to-edge" while the top 48px remains draggable.
 *
 * Cross-platform: `-webkit-app-region` is a Chromium-only CSS extension;
 * regular browsers silently ignore it and the element becomes plain
 * 48px of top breathing room. That makes it safe to keep in shared
 * `packages/views/` without platform branching.
 *
 * Flex child, **not** absolute overlay: `-webkit-app-region` hit-testing
 * with z-index stacking has been empirically unreliable in this codebase
 * (see CLAUDE.md "Drag region" note).
 */
export function DragStrip() {
  return (
    <div
      aria-hidden
      className="h-12 shrink-0"
      style={{ WebkitAppRegion: "drag" } as CSSProperties}
    />
  );
}
