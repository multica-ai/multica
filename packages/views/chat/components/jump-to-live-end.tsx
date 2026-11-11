"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { distanceFromBottom, isAtLiveEnd } from "./stick-to-bottom";
import { useT } from "../../i18n";

// "Jump to latest" for the chat list (MUL-8951).
//
// The button shows once the reader is more than a screen above the live end.
// A jump only has to reach the live end: arriving within its edge zone
// re-engages the stick-to-bottom follow, which keeps the list pinned from
// there as the reply streams on.

/** Most frames a jump keeps re-aiming at the last row while Virtuoso measures it. */
export const JUMP_MAX_FRAMES = 30;

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
    const measure = () => {
      frame = 0;
      setVisible(distanceFromBottom(scrollEl) > scrollEl.clientHeight);
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
  if (hasNewReply) {
    return (
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onClick}
        className={cn(FLOATING, className)}
      >
        <span aria-hidden="true" className="size-1.5 rounded-full bg-brand" />
        {t(($) => $.message_list.new_reply)}
        <ArrowDown />
      </Button>
    );
  }
  return (
    <IconButton
      type="button"
      variant="outline"
      size="icon"
      label={t(($) => $.message_list.jump_to_latest)}
      onClick={onClick}
      className={cn(FLOATING, className)}
    >
      <ArrowDown />
    </IconButton>
  );
}
