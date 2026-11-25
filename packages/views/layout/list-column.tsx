"use client";

import { useRef, useState } from "react";
import { motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { useSidebarSafe } from "@multica/ui/components/ui/sidebar";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useAppShell } from "./app-shell-context";
import { PanelResizeHandle, type PanelWidth } from "./panel-resize-handle";
import { LIST_COLUMN_WIDTH_CLASS, NAV_COLUMN_MOTION_CLASS, NAV_PEEK_CLASS } from "./rail-styles";

const PEEK_EXIT_DURATION = 0.11;

const WIDTH_CLASS: Record<PanelWidth, string> = {
  nav: "w-(--sidebar-width)",
  list: LIST_COLUMN_WIDTH_CLASS,
};

/**
 * The list column of a list-and-detail page (Chat, Settings). It takes
 * the space panel's resize edge and collapse motion. A `nav` column (Settings)
 * also takes the panel's width, so its divider stays put when switching
 * spaces; a `list` column (Chat) shares the wider list width instead,
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
  const reduceMotion = useReducedMotion();
  const onToggle = shell?.toggleNav ?? sidebar?.toggleSidebar;
  // Hovering this space's rail item while collapsed floats the column itself
  // over the page. Only the inner box moves, so the column keeps its width
  // (no collapse motion) and the list keeps its state.
  const peeking = collapsed && !!shell?.peekSpace && shell.peekSpace === shell.space;
  // Derived during render so the column keeps floating on the very frame the
  // peek ends; a post-commit effect would drop it for one frame and skip the
  // exit animation.
  const [prevPeeking, setPrevPeeking] = useState(peeking);
  const [peekExiting, setPeekExiting] = useState(false);
  if (peeking !== prevPeeking) {
    setPrevPeeking(peeking);
    setPeekExiting(!peeking && collapsed && !reduceMotion);
  } else if (peekExiting && (!collapsed || reduceMotion)) {
    setPeekExiting(false);
  }

  const floating = peeking || peekExiting;
  // Outside a peek the inner box rests visible: a collapsing column is
  // clipped by its own shrinking width, not faded.
  const animateStyle =
    peeking && !reduceMotion
      ? { opacity: [0, 1], x: [-8, 0] }
      : peekExiting
        ? { opacity: 0, x: -8 }
        : { opacity: 1, x: 0 };
  const transition = peeking && !reduceMotion
    ? { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }
    : peekExiting
      ? { duration: PEEK_EXIT_DURATION, ease: UI_EASE_IN }
      : { duration: 0 };

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
          !floating && "overflow-hidden",
          className,
        )}
      >
        <motion.div
          data-slot={peeking ? "list-column-peek" : undefined}
          onPointerEnter={peeking ? shell.holdPeek : undefined}
          onPointerLeave={peeking ? shell.releasePeek : undefined}
          initial={false}
          animate={animateStyle}
          transition={transition}
          onAnimationComplete={() => {
            if (peekExiting) setPeekExiting(false);
          }}
          className={cn(
            "flex h-full shrink-0 flex-col overflow-hidden",
            floating ? NAV_PEEK_CLASS : cn("border-r", innerClassName),
            peekExiting && "pointer-events-none",
            WIDTH_CLASS[width],
          )}
        >
          {children}
        </motion.div>
      </aside>
      {!collapsed && sidebar && !sidebar.isCompact && onToggle && (
        <div className="relative w-0 shrink-0">
          <PanelResizeHandle panelRef={ref} onToggle={onToggle} width={width} />
        </div>
      )}
    </>
  );
}
