"use client";

import type { ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { ArrowDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";

/** The gap between the dock and the composer's top edge, in px. */
export const COMPOSER_DOCK_GAP = 12;
const JUMP_COUNT_CAP = 9;

/**
 * What floats over the end of the timeline, just above the composer: the
 * Inbox's group centred (`inboxBar`) and Jump to latest at the right edge,
 * on one line. Neither takes layout. The row is the `@container/inbox-bar`
 * the group's labels give way to, so they collapse before the two can meet.
 */
export function ComposerDock({
  inboxBar,
  jump,
  className,
}: {
  inboxBar?: ReactNode;
  jump: ReactNode;
  /** Where the row sits: over the sticky composer, or pinned to the viewport bottom. */
  className: string;
}) {
  return (
    <div
      data-composer-dock=""
      className={cn(
        "@container/inbox-bar pointer-events-none absolute inset-x-0 flex min-h-10 items-center justify-center",
        className,
      )}
    >
      {inboxBar && <div className="flex min-w-0 max-w-[calc(100%-6rem)] justify-center">{inboxBar}</div>}
      <AnimatePresence initial={false}>{jump}</AnimatePresence>
    </div>
  );
}

/**
 * Jump to latest (MUL-8324, MUL-8951): a round button at the dock's right
 * edge, counting the comments that arrived while the reader was away. Its
 * name says the rest, in the tooltip.
 */
export function JumpToLatestButton({
  onClick,
  label,
  count = 0,
}: {
  onClick: () => void;
  label: string;
  /** Comments that arrived while the reader was away; `label` counts them too. */
  count?: number;
}) {
  const reduceMotion = useReducedMotion();
  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, y: 8, scale: 0.96 }}
      animate={{
        opacity: 1,
        y: 0,
        scale: 1,
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      exit={
        reduceMotion
          ? { opacity: 0, transition: { duration: 0 } }
          : {
              opacity: 0,
              y: 6,
              scale: 0.96,
              transition: { duration: UI_MOTION_DURATION.micro, ease: UI_EASE_IN },
            }
      }
      className="pointer-events-auto absolute right-0 top-1/2 -mt-5"
    >
      <TooltipHint label={label} side="top">
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={label}
          onClick={onClick}
          // eslint-disable-next-line no-restricted-syntax -- jump to latest, round beside the chat launcher's column
          className="relative size-10 rounded-full bg-background shadow-lg dark:bg-background dark:hover:bg-muted"
        >
          <ArrowDown />
          {count > 0 && (
            <span
              aria-hidden
              data-jump-count=""
              className="absolute -right-1 -top-1 flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-brand px-1 text-micro font-medium tabular-nums text-brand-foreground ring-2 ring-background"
            >
              {count > JUMP_COUNT_CAP ? `${JUMP_COUNT_CAP}+` : count}
            </span>
          )}
        </Button>
      </TooltipHint>
    </motion.div>
  );
}
