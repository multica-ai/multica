"use client";

import {
  cloneElement,
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
  type Ref,
} from "react";
import {
  Tooltip,
  TooltipContent,
  TOOLTIP_OPEN_DELAY,
  isTooltipInstantPhase,
} from "@multica/ui/components/ui/tooltip";

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
  shouldOpen,
  openOnFocus = false,
  contentClassName,
}: {
  /** The visible trigger element (e.g. an icon button). Cloned with hover
   *  handlers and an anchor ref; its own props are preserved. */
  trigger: ReactElement<Record<string, unknown>>;
  content: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
  /** Checked against the trigger element when the open delay elapses; the
   *  tooltip stays closed when it returns false. */
  shouldOpen?: (el: HTMLElement) => boolean;
  /** Also open on keyboard focus (and close on blur), gated by the same
   *  checks as hover. Focus that follows a pointer press stays closed. */
  openOnFocus?: boolean;
  contentClassName?: string;
}) {
  const anchorRef = useRef<HTMLElement | null>(null);
  const [warm, setWarm] = useState(false);
  const [open, setOpen] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Set by a pointer press so the focus it causes is not treated as keyboard focus.
  const pointerFocusRef = useRef(false);

  const clearTimer = () => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };
  useEffect(() => clearTimer, []);

  const canOpen = () => {
    // A trigger whose own menu/popover is open keeps the tooltip hidden;
    // plain expand/collapse toggles (aria-expanded without a popup) keep it.
    const el = anchorRef.current;
    if (!el) return false;
    const popupOpen =
      el.hasAttribute("data-popup-open") ||
      (el.getAttribute("aria-expanded") === "true" &&
        (el.getAttribute("aria-haspopup") ?? "false") !== "false");
    if (popupOpen) return false;
    return !shouldOpen || shouldOpen(el);
  };

  const triggerRef = trigger.props.ref as Ref<HTMLElement> | undefined;
  const element = cloneElement(trigger, {
    ref: (el: HTMLElement | null) => {
      anchorRef.current = el;
      if (typeof triggerRef === "function") triggerRef(el);
      else if (triggerRef) (triggerRef as { current: HTMLElement | null }).current = el;
    },
    onPointerEnter: (e: React.PointerEvent) => {
      (trigger.props.onPointerEnter as ((e: React.PointerEvent) => void) | undefined)?.(e);
      setWarm(true);
      clearTimer();
      // Same delay as every other tooltip, and the same instant hand-off: when
      // a tooltip is showing or just closed, open without waiting.
      timerRef.current = setTimeout(
        () => {
          if (canOpen()) setOpen(true);
        },
        isTooltipInstantPhase() ? 0 : TOOLTIP_OPEN_DELAY,
      );
    },
    onPointerLeave: (e: React.PointerEvent) => {
      (trigger.props.onPointerLeave as ((e: React.PointerEvent) => void) | undefined)?.(e);
      clearTimer();
      setOpen(false);
    },
    onPointerDown: (e: React.PointerEvent) => {
      (trigger.props.onPointerDown as ((e: React.PointerEvent) => void) | undefined)?.(e);
      // The press focuses the trigger in this same task; reset afterwards so
      // a press that moves no focus can't swallow later keyboard focus.
      pointerFocusRef.current = true;
      setTimeout(() => {
        pointerFocusRef.current = false;
      }, 0);
      clearTimer();
      setOpen(false);
    },
    ...(openOnFocus
      ? {
          onFocus: (e: React.FocusEvent) => {
            (trigger.props.onFocus as ((e: React.FocusEvent) => void) | undefined)?.(e);
            const fromPointer = pointerFocusRef.current;
            pointerFocusRef.current = false;
            if (fromPointer) return;
            setWarm(true);
            clearTimer();
            if (canOpen()) setOpen(true);
          },
          onBlur: (e: React.FocusEvent) => {
            (trigger.props.onBlur as ((e: React.FocusEvent) => void) | undefined)?.(e);
            pointerFocusRef.current = false;
            clearTimer();
            setOpen(false);
          },
        }
      : {}),
  });

  return (
    <>
      {element}
      {warm && (
        <Tooltip open={open} onOpenChange={setOpen}>
          <TooltipContent side={side} anchor={anchorRef} className={contentClassName}>
            {content}
          </TooltipContent>
        </Tooltip>
      )}
    </>
  );
}
