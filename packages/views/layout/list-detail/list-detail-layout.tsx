"use client";

import { useEffect, type ReactNode } from "react";
import { usePanelRef } from "react-resizable-panels";
import { useListDetailSplitStore } from "@multica/core/list-detail/stores";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";

const DEFAULT_RAIL_SIZE = 320;

interface ListDetailLayoutProps {
  /** Left column: a `ListDetailRail` (or any column content for that slot). */
  rail: ReactNode;
  /** Right column: the detail page content. */
  detail: ReactNode;
}

/**
 * Three-state wrapper for the Autopilots / Agents detail pages:
 *
 * - Below the desktop breakpoint: the previous full-width detail, no rail.
 * - Collapsed (the persisted default): a fixed 44px rail (expand button +
 *   count) beside the detail, matching the inbox/chat two-panel layout
 *   without a resize handle.
 * - Expanded: a `ResizablePanelGroup` with the list on the left (default 320,
 *   clamped 240-480) and the detail on the right (min 40%). The dragged
 *   width is persisted through the split store and re-applied on mount, so a
 *   refresh keeps the width the user set.
 *
 * The rail/detail are rendered at a fixed position and never keyed by the
 * active item id, so switching rows in place keeps both columns mounted and
 * their scroll positions intact.
 */
export function ListDetailLayout({ rail, detail }: ListDetailLayoutProps) {
  const isCompact = useIsCompact();
  const collapsed = useListDetailSplitStore((s) => s.collapsed);
  const size = useListDetailSplitStore((s) => s.size);
  const setSize = useListDetailSplitStore((s) => s.setSize);
  const listPanelRef = usePanelRef();

  // The store rehydrates asynchronously after a workspace switch, so on a
  // fresh page load the panel may mount with the default width and only later
  // learn the persisted one. Push the stored width into the panel whenever it
  // differs from the panel's current width (idempotent, so drags converge).
  useEffect(() => {
    const panel = listPanelRef.current;
    if (!panel || size === undefined) return;
    const current = panel.getSize();
    if (current.inPixels !== size) {
      panel.resize(size);
    }
  }, [size, listPanelRef]);

  // Below the desktop breakpoint keep the current full-width detail and skip
  // the left rail entirely.
  if (isCompact) {
    return <>{detail}</>;
  }

  // Collapsed: a fixed narrow rail (icon + count) beside the full detail,
  // matching the inbox/chat two-panel layout without a resize handle.
  if (collapsed) {
    return (
      <div className="flex flex-1 min-h-0">
        <div className="flex w-11 shrink-0 flex-col border-r">{rail}</div>
        <div className="flex min-h-0 flex-1 flex-col">{detail}</div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0">
      <ResizablePanel
        id="list"
        panelRef={listPanelRef}
        defaultSize={size ?? DEFAULT_RAIL_SIZE}
        minSize={240}
        maxSize={480}
        onResize={({ inPixels }) => setSize(inPixels)}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="flex h-full flex-col border-r">{rail}</div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
        <div className="flex h-full min-h-0 flex-col">{detail}</div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
