import { useLayoutEffect, useRef, useState } from "react";
import { prefersReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

function fold(element: HTMLElement | null, open: boolean) {
  const slot = element?.closest<HTMLElement>("[data-issue-sidebar-section]");
  if (!slot?.animate || prefersReducedMotion()) return undefined;
  const height = `${slot.getBoundingClientRect().height}px`;
  const frames = [
    { height: "0px", opacity: 0, overflow: "hidden" },
    { height, opacity: 1, overflow: "hidden" },
  ];
  return slot.animate(open ? frames : frames.reverse(), {
    duration: (open ? UI_MOTION_DURATION.fast : UI_MOTION_DURATION.micro) * 1000,
    easing: UI_EASE_OUT_CSS,
    // Held shut until the section unmounts, which cancels it.
    fill: open ? "none" : "forwards",
  });
}

/**
 * Opens a revealed section's form again when the reader picks the section
 * while it is still folding away: it stays mounted then, so its initial open
 * state has already been used. `open` must set the caller's own state.
 */
export function useReopenOnReveal(reveal: boolean, open: () => void) {
  const [revealed, setRevealed] = useState(reveal);
  if (reveal !== revealed) {
    setRevealed(reveal);
    if (reveal) open();
  }
}

/**
 * Height motion for a section the add row reveals while it is empty. Its slot
 * grows open with it, and when the reveal ends with the section still empty
 * the slot folds shut before the section goes, so the rows below slide
 * instead of jumping. Whether it is still empty is read on the render after
 * the reveal ends: a create that just finished may land in that render.
 */
export function useSidebarRevealMotion<T extends HTMLElement>(
  reveal: boolean,
  hasContent: boolean,
  onRevealEnd?: () => void,
) {
  const ref = useRef<T>(null);
  const [leaving, setLeaving] = useState(false);
  useLayoutEffect(() => {
    if (!reveal) return;
    const animation = fold(ref.current, true);
    return () => animation?.cancel();
  }, [reveal]);
  const folding = leaving && !reveal && !hasContent;
  useLayoutEffect(() => {
    if (!leaving) return;
    const animation = folding ? fold(ref.current, false) : undefined;
    if (!animation) {
      setLeaving(false);
      return;
    }
    let current = true;
    animation.finished.then(() => current && setLeaving(false), () => {});
    return () => {
      current = false;
      animation.cancel();
    };
  }, [leaving, folding]);
  return {
    ref,
    shown: hasContent || reveal || leaving,
    end: () => {
      setLeaving(true);
      onRevealEnd?.();
    },
  };
}
