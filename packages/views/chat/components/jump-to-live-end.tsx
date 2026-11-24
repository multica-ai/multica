"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { ArrowDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { distanceFromBottom, isAtLiveEnd } from "./stick-to-bottom";
import { useT } from "../../i18n";

// "Jump to latest" for the chat list (MUL-8951).
//
// The button shows once the reader is more than a screen above the live end,
// and stays until they are back within half a screen of it, so it does not
// flicker at the threshold. A jump only has to reach the live end: arriving
// within its edge zone
// re-engages the stick-to-bottom follow, which keeps the list pinned from
// there as the reply streams on.

/** Most frames a jump keeps re-aiming at the last row while Virtuoso measures it. */
export const JUMP_MAX_FRAMES = 30;

/** How close to the live end, in screens, hides the button once shown. */
const RETURN_WITHIN_SCREENS = 0.5;

export interface JumpToLiveEnd {
  visible: boolean;
  /** The agent replied while the reader was away from the live end. */
  hasNewReply: boolean;
  jump(): void;
}

/**
 * `pinToLiveEnd` must scroll through Virtuoso (see `useStickToBottom`): the
 * container's `scrollHeight` is an estimate, so a jump re-aims each frame
 * until the list holds at the live end. `tailKey` changes whenever the last
 * row is new or has grown, and `tailIsReply` says whether that row is the
 * agent's — together they flag a reply landing out of view.
 */
export function useJumpToLiveEnd(
  scrollEl: HTMLElement | null,
  pinToLiveEnd: () => void,
  {
    enabled,
    tailKey,
    tailIsReply,
  }: { enabled: boolean; tailKey: string | null; tailIsReply: boolean },
): JumpToLiveEnd {
  const [visible, setVisible] = useState(false);
  const [hasNewReply, setHasNewReply] = useState(false);
  const visibleRef = useRef(visible);
  visibleRef.current = visible;
  const cancelJumpRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!enabled || !scrollEl) {
      setVisible(false);
      return;
    }
    let frame = 0;
    let shown = false;
    const measure = () => {
      frame = 0;
      const threshold = shown ? scrollEl.clientHeight * RETURN_WITHIN_SCREENS : scrollEl.clientHeight;
      shown = distanceFromBottom(scrollEl) > threshold;
      setVisible(shown);
    };
    const schedule = () => {
      if (frame === 0) frame = requestAnimationFrame(measure);
    };
    measure();
    scrollEl.addEventListener("scroll", schedule, { passive: true });
    // Streaming growth and older pages change the extent without a scroll event.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(scrollEl);
    for (const child of Array.from(scrollEl.children)) observer?.observe(child);
    return () => {
      scrollEl.removeEventListener("scroll", schedule);
      observer?.disconnect();
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, [enabled, scrollEl]);

  const lastTailKeyRef = useRef(tailKey);
  useEffect(() => {
    if (lastTailKeyRef.current === tailKey) return;
    lastTailKeyRef.current = tailKey;
    if (visibleRef.current && tailIsReply) setHasNewReply(true);
  }, [tailKey, tailIsReply]);

  useEffect(() => {
    if (!visible) setHasNewReply(false);
  }, [visible]);

  useEffect(() => () => cancelJumpRef.current?.(), []);

  const jump = useCallback(() => {
    const el = scrollEl;
    if (!el) return;
    cancelJumpRef.current?.();
    let frame = 0;
    let frames = 0;
    let cancelled = false;
    const userInputs = ["wheel", "touchstart", "pointerdown"] as const;
    const stop = () => {
      cancelled = true;
      cancelAnimationFrame(frame);
      for (const type of userInputs) el.removeEventListener(type, stop);
      if (cancelJumpRef.current === stop) cancelJumpRef.current = null;
    };
    const tick = () => {
      if (cancelled) return;
      if (!el.isConnected || isAtLiveEnd(el) || ++frames >= JUMP_MAX_FRAMES) {
        stop();
        return;
      }
      pinToLiveEnd();
      frame = requestAnimationFrame(tick);
    };
    for (const type of userInputs) el.addEventListener(type, stop, { passive: true });
    cancelJumpRef.current = stop;
    setHasNewReply(false);
    pinToLiveEnd();
    frame = requestAnimationFrame(tick);
  }, [scrollEl, pinToLiveEnd]);

  return { visible, hasNewReply, jump };
}

const FLOATING = "rounded-full bg-background shadow-md dark:bg-background dark:hover:bg-muted";

export function JumpToLiveEndButton({
  hasNewReply,
  onClick,
  className,
}: {
  hasNewReply: boolean;
  onClick: () => void;
  className?: string;
}) {
  const { t } = useT("chat");
  const reduceMotion = useReducedMotion();
  const label = hasNewReply
    ? t(($) => $.message_list.new_reply)
    : t(($) => $.message_list.jump_to_latest);

  const button = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      aria-label={hasNewReply ? undefined : label}
      onClick={onClick}
      className={cn(
        FLOATING,
        "pointer-events-auto gap-0 transition-[padding] duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
        hasNewReply ? "h-8 px-3" : "size-8 px-0",
      )}
    >
      <AnimatePresence initial={false}>
        {hasNewReply && (
          <motion.span
            key="new-reply-pill"
            initial={reduceMotion ? false : { opacity: 0, width: 0 }}
            animate={{
              opacity: 1,
              width: "auto",
              transition: { duration: reduceMotion ? 0 : 0.16, ease: UI_EASE_OUT },
            }}
            exit={
              reduceMotion
                ? { opacity: 0, transition: { duration: 0 } }
                : {
                    opacity: 0,
                    width: 0,
                    transition: { duration: UI_MOTION_DURATION.micro, ease: UI_EASE_IN },
                  }
            }
            className="inline-flex items-center overflow-hidden whitespace-nowrap"
          >
            <span aria-hidden="true" className="mr-1.5 size-1.5 shrink-0 rounded-full bg-brand" />
            <span className="mr-1.5">{t(($) => $.message_list.new_reply)}</span>
          </motion.span>
        )}
      </AnimatePresence>
      <ArrowDown className="shrink-0" />
    </Button>
  );

  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, y: 8, scale: 0.92 }}
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
              scale: 0.92,
              transition: { duration: UI_MOTION_DURATION.micro, ease: UI_EASE_IN },
            }
      }
      className={cn("pointer-events-none", className)}
    >
      {hasNewReply ? button : <TooltipHint label={label}>{button}</TooltipHint>}
    </motion.div>
  );
}
