"use client";

import type { ReactNode } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { Virtuoso, type Components } from "react-virtuoso";
import { useReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import type { ChatSession } from "@multica/core/types";

type ListContext = { footer?: ReactNode };

/**
 * Up to this many rows, `animateExit` renders a plain scrollable list inside
 * `AnimatePresence` so archiving/deleting a row collapses its height smoothly
 * instead of snapping the rows below upward. Longer lists stay virtualized so
 * mount cost remains bounded (mirrors `InboxSection`'s virtualization boundary).
 */
export const SESSION_LIST_VIRTUALIZE_AFTER = 60;

// Keep component identities stable, including when there is no archive entry.
// Passing an explicit undefined components prop breaks Virtuoso 4.18 builds.
const COMPONENTS: Components<ChatSession, ListContext> = {
  Footer: ({ context }) => <>{context?.footer}</>,
};

function SessionExitRow({ children }: { children: ReactNode }) {
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion();

  return (
    <motion.div
      data-chat-row-exiting={!isPresent ? "" : undefined}
      aria-hidden={!isPresent || undefined}
      initial={false}
      exit={{
        opacity: 0,
        height: 0,
        overflow: "hidden",
        transition: {
          duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast,
          ease: UI_EASE_OUT,
        },
      }}
      className={cn(!isPresent && "pointer-events-none")}
    >
      {children}
    </motion.div>
  );
}

export function SessionHistoryList({
  sessions,
  renderRow,
  footer,
  maxHeight,
  estimatedRowHeight = 56,
  animateExit = false,
}: {
  sessions: ChatSession[];
  renderRow: (session: ChatSession) => ReactNode;
  footer?: ReactNode;
  maxHeight?: number;
  estimatedRowHeight?: number;
  /** Smoothly collapse rows as they leave the list (for small/medium lists). */
  animateExit?: boolean;
}) {
  const height =
    maxHeight == null
      ? "100%"
      : Math.min(maxHeight, sessions.length * estimatedRowHeight);

  if (animateExit && sessions.length <= SESSION_LIST_VIRTUALIZE_AFTER) {
    return (
      <div className="overflow-y-auto" style={{ height }}>
        <AnimatePresence initial={false}>
          {sessions.map((session) => (
            <SessionExitRow key={session.id}>
              {renderRow(session)}
            </SessionExitRow>
          ))}
        </AnimatePresence>
        {footer}
      </div>
    );
  }

  return (
    <Virtuoso
      style={{ height }}
      data={sessions}
      defaultItemHeight={estimatedRowHeight}
      increaseViewportBy={200}
      computeItemKey={(_, session) => session.id}
      itemContent={(_, session) => renderRow(session)}
      components={COMPONENTS}
      context={{ footer }}
    />
  );
}
