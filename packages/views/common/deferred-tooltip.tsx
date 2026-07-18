"use client";

import {
  cloneElement,
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { Tooltip, TooltipContent } from "@multica/ui/components/ui/tooltip";

// Matches the ui TooltipProvider default delay so the deferred first-open
// feels identical to a native tooltip.
const TOOLTIP_OPEN_DELAY = 200;

/**
 * Tooltip whose machinery mounts only on first hover.
 *
 * Dense grids (board/swimlane column headers) mount hundreds of tooltip
 * roots that are almost never hovered — a measurable slice of surface mount
 * cost. This wrapper renders ONLY the trigger element until the pointer
 * first enters it, then mounts a controlled Tooltip anchored to that same
 * element.
 *
 * Unlike a trigger-swap approach, the trigger element is rendered by this
 * component in the same tree position before and after warming — its DOM
 * node is preserved, so mid-gesture events (the hover's own click) never
 * land on a detached node. Hover open/close is driven here (timer on enter,
 * close on leave); the popup positions against the trigger via an anchor
 * ref instead of a Base UI trigger wrapper.
 */
export function DeferredTooltip({
  trigger,
  content,
  side,
}: {
  /** The visible trigger element (e.g. an icon button). Cloned with hover
   *  handlers and an anchor ref; its own props are preserved. */
  trigger: ReactElement<Record<string, unknown>>;
  content: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
}) {
  const anchorRef = useRef<HTMLElement | null>(null);
  const [warm, setWarm] = useState(false);
  const [open, setOpen] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTimer = () => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };
  useEffect(() => clearTimer, []);

  const element = cloneElement(trigger, {
    ref: (el: HTMLElement | null) => {
      anchorRef.current = el;
    },
    onPointerEnter: (e: React.PointerEvent) => {
      (trigger.props.onPointerEnter as ((e: React.PointerEvent) => void) | undefined)?.(e);
      setWarm(true);
      clearTimer();
      timerRef.current = setTimeout(() => setOpen(true), TOOLTIP_OPEN_DELAY);
    },
    onPointerLeave: (e: React.PointerEvent) => {
      (trigger.props.onPointerLeave as ((e: React.PointerEvent) => void) | undefined)?.(e);
      clearTimer();
      setOpen(false);
    },
  });

  return (
    <>
      {element}
      {warm && (
        <Tooltip open={open} onOpenChange={setOpen}>
          <TooltipContent side={side} anchor={anchorRef}>
            {content}
          </TooltipContent>
        </Tooltip>
      )}
    </>
  );
}
