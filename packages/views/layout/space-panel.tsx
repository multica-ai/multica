"use client";
import { DocTree } from "../docs/components/doc-tree";

import { useEffect, useRef } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { useCurrentWorkspace, type AppSpaceId } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import { useAppShell } from "./app-shell-context";
import { PanelResizeHandle } from "./panel-resize-handle";
import { AutopilotsPanel } from "./panels/autopilots-panel";
import { GuestWorkPanel } from "./panels/guest-work-panel";
import { ChatPeek } from "./panels/list-space-peeks";
import { LIST_COLUMN_WIDTH_CLASS, NAV_COLUMN_MOTION_CLASS, NAV_PEEK_CLASS } from "./rail-styles";
import { TeamPanel } from "./panels/team-panel";
import { KeptRowsRelease, WorkPanel } from "./panels/work-panel";

/** A guest (MUL-9420) sees its own Work panel: only what is shared with it. */
function WorkSpacePanel() {
  const guest = useIsGuest(useCurrentWorkspace()?.id);
  return guest ? <GuestWorkPanel /> : <WorkPanel />;
}

const PANELS: Partial<Record<AppSpaceId, () => React.ReactNode>> = {
  work: WorkSpacePanel,
  docs: DocTree,
  team: TeamPanel,
  autopilots: AutopilotsPanel,
};

// Chat keeps its list in its page, so from another space it previews through
// a standalone copy of the list, at the list width.
const LIST_PEEKS: Partial<Record<AppSpaceId, () => React.ReactNode>> = {
  chat: ChatPeek,
};
const PEEKS: Partial<Record<AppSpaceId, () => React.ReactNode>> = {
  ...PANELS,
  ...LIST_PEEKS,
};

/** The current space's panel content, or null for spaces without one. */
export function SpacePanelContent({ space }: { space: AppSpaceId | null }) {
  const Panel = space ? PANELS[space] : undefined;
  return Panel ? <Panel /> : null;
}

const PEEK_EXIT_DURATION = 0.11;

function SpacePanelPeek({
  isListPeek,
  reduceMotion,
  onPointerEnter,
  onPointerLeave,
  children,
}: {
  isListPeek: boolean;
  reduceMotion: boolean;
  onPointerEnter: () => void;
  onPointerLeave: () => void;
  children: React.ReactNode;
}) {
  const isPresent = useIsPresent();
  return (
    <motion.aside
      data-slot="space-panel-peek"
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
      initial={reduceMotion ? false : { opacity: 0, x: -8 }}
      animate={{
        opacity: 1,
        x: 0,
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      exit={{
        opacity: 0,
        x: reduceMotion ? 0 : -8,
        transition: { duration: reduceMotion ? 0 : PEEK_EXIT_DURATION, ease: UI_EASE_IN },
      }}
      className={cn(
        NAV_PEEK_CLASS,
        "max-md:hidden",
        !isPresent && "pointer-events-none",
        isListPeek && LIST_COLUMN_WIDTH_CLASS,
      )}
    >
      {children}
    </motion.aside>
  );
}

/**
 * The space panel inside the content card: a column on the card's leading
 * edge, separated from the page by one hairline. Spaces without a shell
 * panel (Chat, Settings) render their own `ListColumn` instead, so
 * nothing mounts for them. While collapsed, hovering a rail space previews
 * its panel over the page.
 */
export function SpacePanel() {
  const shell = useAppShell();
  const { open, isCompact } = useSidebar();
  const reduceMotion = useReducedMotion();
  const panelRef = useRef<HTMLElement>(null);
  useEffect(() => {
    // Public state hook for themes (docs/theming.md).
    const root = document.documentElement;
    root.classList.toggle("is-sidebar-collapsed", !open);
    return () => root.classList.remove("is-sidebar-collapsed");
  }, [open]);
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
          data-mc="sidebar"
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
      {navCollapsed && (
        <AnimatePresence>
          {Peek && (
            <SpacePanelPeek
              key="space-panel-peek"
              isListPeek={Boolean(peekSpace && LIST_PEEKS[peekSpace])}
              reduceMotion={reduceMotion}
              onPointerEnter={shell.holdPeek}
              onPointerLeave={shell.releasePeek}
            >
              <Peek />
            </SpacePanelPeek>
          )}
        </AnimatePresence>
      )}
    </>
  );
}
