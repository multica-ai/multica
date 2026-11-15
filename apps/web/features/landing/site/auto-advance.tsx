"use client";

import { useEffect, useRef, useState, type FocusEvent, type PointerEvent } from "react";
import { cn } from "@multica/ui/lib/utils";
import { usePrefersReducedMotion } from "./demo/mock-kit";

/**
 * Cycles through a set of tabs while they sit in the middle of the viewport.
 * The active tab's `AdvanceLine` is the timer: the next tab comes up when its
 * fill animation ends, so holding the line holds the cycle. It holds while
 * the block is off screen and while the pointer or focus is on the tab list
 * (spread `holdProps` there, not on the whole block: a resting cursor ends up
 * over the block as the page scrolls). Picking a tab or otherwise interacting
 * hands control to the reader and stops the cycle for good.
 */
export function useAutoAdvance(count: number) {
  const ref = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(0);
  const [auto, setAuto] = useState(true);
  const [visible, setVisible] = useState(false);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const intersection = new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? false), {
      rootMargin: "-25% 0px -25% 0px",
    });
    intersection.observe(el);
    return () => intersection.disconnect();
  }, []);

  const playing = auto && !reducedMotion;

  return {
    ref,
    active,
    playing,
    running: playing && visible && !hovered && !focused,
    select(index: number) {
      setAuto(false);
      setActive(index);
    },
    advance() {
      setActive((current) => (current + 1) % count);
    },
    stop() {
      setAuto(false);
    },
    holdProps: {
      onPointerEnter: (event: PointerEvent) => event.pointerType === "mouse" && setHovered(true),
      onPointerLeave: () => setHovered(false),
      onFocus: () => setFocused(true),
      onBlur: (event: FocusEvent) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false);
      },
    },
  };
}

/** The rule above a tab: fills over `duration` ms while the cycle plays, or marks the reader's pick once it stops. */
export function AdvanceLine({
  selected,
  playing,
  running,
  duration,
  onDone,
}: {
  selected: boolean;
  playing: boolean;
  running: boolean;
  duration: number;
  onDone: () => void;
}) {
  const timing = selected && playing;
  return (
    <span
      aria-hidden
      onAnimationEnd={timing ? onDone : undefined}
      className={cn(
        "absolute -top-px left-0 h-px w-full origin-left bg-site-ink",
        !playing && "transition-transform duration-500 ease-out motion-reduce:transition-none",
        !timing && (selected ? "scale-x-100" : "scale-x-0"),
      )}
      style={
        timing
          ? {
              animationName: "site-advance",
              animationDuration: `${duration}ms`,
              animationTimingFunction: "linear",
              animationFillMode: "both",
              animationPlayState: running ? "running" : "paused",
            }
          : undefined
      }
    />
  );
}
