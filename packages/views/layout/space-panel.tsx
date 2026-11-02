"use client";

import { useRef } from "react";
import type { AppSpaceId } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import { useAppShell } from "./app-shell-context";
import { PanelResizeHandle } from "./panel-resize-handle";
import { AutopilotsPanel } from "./panels/autopilots-panel";
import { ChatPeek, InboxPeek } from "./panels/list-space-peeks";
import { NAV_COLUMN_MOTION_CLASS, NAV_PEEK_CLASS } from "./rail-styles";
import { TeamPanel } from "./panels/team-panel";
import { KeptRowsRelease, WorkPanel } from "./panels/work-panel";

const PANELS: Partial<Record<AppSpaceId, () => React.ReactNode>> = {
  work: WorkPanel,
  team: TeamPanel,
  autopilots: AutopilotsPanel,
};

// Inbox and Chat keep their lists in their pages, so from another space they
// preview through a standalone copy of the list.
const PEEKS: Partial<Record<AppSpaceId, () => React.ReactNode>> = {
  ...PANELS,
  inbox: InboxPeek,
  chat: ChatPeek,
};

/** The current space's panel content, or null for spaces without one. */
export function SpacePanelContent({ space }: { space: AppSpaceId | null }) {
  const Panel = space ? PANELS[space] : undefined;
  return Panel ? <Panel /> : null;
}

/**
 * The space panel inside the content card: a column on the card's leading
 * edge, separated from the page by one hairline. Spaces without a shell
 * panel (Inbox, Chat, Settings) render their own `ListColumn` at the same
 * width instead, so nothing mounts for them. While collapsed, hovering a rail space previews
 * its panel over the page.
 */
export function SpacePanel() {
  const shell = useAppShell();
  const { open, isCompact } = useSidebar();
  const panelRef = useRef<HTMLElement>(null);
  if (!shell) return null;
  if (isCompact) return <KeptRowsRelease />;

  const { space, hasPanel, peekSpace, navCollapsed } = shell;
  // The current space's own list column floats itself (`ListColumn`).
  const Peek =
    navCollapsed && peekSpace && (hasPanel || peekSpace !== space) ? PEEKS[peekSpace] : undefined;

  return (
    <>
      <KeptRowsRelease />
      {hasPanel && (
        <aside
          // A new space mounts at its final width; only toggling animates.
          key={space}
          ref={panelRef}
          data-slot="space-panel"
          data-state={open ? "expanded" : "collapsed"}
          inert={!open}
          className={cn(
            "relative hidden shrink-0 justify-end overflow-hidden md:flex",
            NAV_COLUMN_MOTION_CLASS,
            open ? "w-(--sidebar-width)" : "w-0",
          )}
        >
          <div className="flex h-full w-(--sidebar-width) shrink-0 flex-col overflow-hidden border-r">
            <SpacePanelContent space={space} />
          </div>
        </aside>
      )}
      {hasPanel && open && (
        <div className="relative hidden w-0 shrink-0 md:block">
          <PanelResizeHandle panelRef={panelRef} onToggle={shell.toggleNav} />
        </div>
      )}
      {Peek && (
        <aside
          data-slot="space-panel-peek"
          onPointerEnter={shell.holdPeek}
          onPointerLeave={shell.releasePeek}
          className={cn(NAV_PEEK_CLASS, "max-md:hidden")}
        >
          <Peek />
        </aside>
      )}
    </>
  );
}
