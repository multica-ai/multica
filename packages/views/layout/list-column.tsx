"use client";

import { useRef } from "react";
import { useSidebarSafe } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { useAppShell } from "./app-shell-context";
import { PanelResizeHandle, type PanelWidth } from "./panel-resize-handle";
import { LIST_COLUMN_WIDTH_CLASS, NAV_COLUMN_MOTION_CLASS, NAV_PEEK_CLASS } from "./rail-styles";

const WIDTH_CLASS: Record<PanelWidth, string> = {
  nav: "w-(--sidebar-width)",
  list: LIST_COLUMN_WIDTH_CLASS,
};

/**
 * The list column of a list-and-detail page (Inbox, Chat, Settings). It takes
 * the space panel's resize edge and collapse motion. A `nav` column (Settings)
 * also takes the panel's width, so its divider stays put when switching
 * spaces; a `list` column (Inbox, Chat) shares the wider list width instead,
 * because its rows carry far more than the nav's labels. It stays mounted
 * while collapsed, keeping the list's scroll position.
 */
export function ListColumn({
  collapsed = false,
  width = "nav",
  className,
  innerClassName,
  children,
}: {
  collapsed?: boolean;
  width?: PanelWidth;
  className?: string;
  innerClassName?: string;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);
  const sidebar = useSidebarSafe();
  const shell = useAppShell();
  const onToggle = shell?.toggleNav ?? sidebar?.toggleSidebar;
  // Hovering this space's rail item while collapsed floats the column itself
  // over the page. Only the inner box moves, so the column keeps its width
  // (no collapse motion) and the list keeps its state.
  const peeking = collapsed && !!shell?.peekSpace && shell.peekSpace === shell.space;

  return (
    <>
      <aside
        ref={ref}
        data-slot="list-column"
        data-state={collapsed ? "collapsed" : "expanded"}
        inert={collapsed && !peeking}
        className={cn(
          "relative flex h-full shrink-0 justify-end",
          NAV_COLUMN_MOTION_CLASS,
          collapsed ? "w-0" : WIDTH_CLASS[width],
          !peeking && "overflow-hidden",
          className,
        )}
      >
        <div
          data-slot={peeking ? "list-column-peek" : undefined}
          onPointerEnter={peeking ? shell.holdPeek : undefined}
          onPointerLeave={peeking ? shell.releasePeek : undefined}
          className={cn(
            "flex h-full shrink-0 flex-col overflow-hidden",
            peeking ? NAV_PEEK_CLASS : cn("border-r", innerClassName),
            WIDTH_CLASS[width],
          )}
        >
          {children}
        </div>
      </aside>
      {!collapsed && sidebar && !sidebar.isCompact && onToggle && (
        <div className="relative w-0 shrink-0">
          <PanelResizeHandle panelRef={ref} onToggle={onToggle} width={width} />
        </div>
      )}
    </>
  );
}
