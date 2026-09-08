import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from "react";
import type { AgentTask } from "@multica/core/types";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

const EMPTY_IDS: ReadonlySet<string> = new Set();

function reveal(element: HTMLElement | null, duration: number, translate = 0) {
  if (!element?.animate) return;
  const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  if (reduced) return;
  return element.animate(
    translate
      ? [{ opacity: 0, transform: `translateY(${translate}px)` }, { opacity: 1, transform: "translateY(0)" }]
      : [{ opacity: 0 }, { opacity: 1 }],
    { duration, easing: UI_EASE_OUT_CSS },
  );
}

/** Mark additions after the initial query, not existing history or route restoration. */
export function useNewRunIds(issueId: string, tasks: readonly AgentTask[] | undefined) {
  const known = useRef<{ issueId: string; ids: Set<string> } | null>(null);
  const [arrivals, setArrivals] = useState<{ issueId: string; ids: ReadonlySet<string> }>({ issueId, ids: EMPTY_IDS });
  useEffect(() => {
    if (!tasks) return;
    if (!known.current || known.current.issueId !== issueId) {
      known.current = { issueId, ids: new Set(tasks.map((task) => task.id)) };
      setArrivals((previous) => previous.issueId === issueId && previous.ids === EMPTY_IDS
        ? previous : { issueId, ids: EMPTY_IDS });
      return;
    }
    const added = tasks.filter((task) => !known.current!.ids.has(task.id));
    if (added.length === 0) return;
    for (const task of added) known.current.ids.add(task.id);
    setArrivals((previous) => ({ issueId, ids: new Set([
      ...(previous.issueId === issueId ? previous.ids : EMPTY_IDS), ...added.map((task) => task.id),
    ]) }));
  }, [issueId, tasks]);
  return arrivals.issueId === issueId ? arrivals.ids : EMPTY_IDS;
}

/** Pause ambient run motion outside the viewport, including Virtuoso overscan. */
export function useRunAnimationVisibility<T extends Element>() {
  const [element, setElement] = useState<T | null>(null);
  const [visible, setVisible] = useState(true);
  const ref = useCallback((node: T | null) => setElement(node), []);
  useEffect(() => {
    if (!element || typeof IntersectionObserver !== "function") return;
    const observer = new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? true));
    observer.observe(element);
    return () => observer.disconnect();
  }, [element]);
  return { ref, visible };
}

/** Animate observed changes only. A virtualized row's first mount is always still. */
export function useRunCommentMotion(entering: boolean, replyId: string | undefined, status: string) {
  const ref = useRef<HTMLDivElement>(null);
  const previous = useRef({ entering, replyId, status });
  useLayoutEffect(() => {
    const before = previous.current;
    previous.current = { entering, replyId, status };
    const animations: (Animation | undefined)[] = [];
    if (entering && !before.entering) {
      animations.push(reveal(ref.current, UI_MOTION_DURATION.fast * 1000, 4));
    } else if (replyId && replyId !== before.replyId) {
      const body = Array.from(ref.current?.querySelectorAll<HTMLElement>("[data-comment-content]") ?? [])
        .find((element) => element.dataset.commentContent === replyId);
      animations.push(reveal(body ?? null, UI_MOTION_DURATION.fast * 1000));
    } else if (status !== before.status) {
      animations.push(reveal(ref.current?.querySelector("[data-run-status]") ?? null, UI_MOTION_DURATION.micro * 1000));
    }
    return () => { for (const animation of animations) animation?.cancel(); };
  }, [entering, replyId, status]);
  return ref;
}

/** Animate the toggle itself, including when historical runs mount a new trigger. */
export function useRunDisclosureMotion(open: boolean) {
  const chevronRef = useRef<SVGSVGElement>(null);
  const pending = useRef(false);
  const focusedTrigger = useRef<HTMLElement | null>(null);
  const fromRotation = useRef("0deg");
  useLayoutEffect(() => {
    const requested = pending.current;
    pending.current = false;
    const previousTrigger = focusedTrigger.current;
    focusedTrigger.current = null;
    if (previousTrigger && !previousTrigger.isConnected && document.activeElement === document.body) {
      chevronRef.current?.closest<HTMLElement>("button, summary")?.focus({ preventScroll: true });
    }
    if (!requested) return;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    if (reduced) return;
    const duration = (open ? UI_MOTION_DURATION.fast : UI_MOTION_DURATION.micro) * 1000;
    const arrow = chevronRef.current?.animate?.(
      [{ rotate: fromRotation.current }, { rotate: open ? "90deg" : "0deg" }],
      { duration, easing: UI_EASE_OUT_CSS },
    );
    return () => { arrow?.cancel(); };
  }, [open]);
  return {
    chevronRef,
    onTrigger(event: MouseEvent<HTMLElement>) {
      pending.current = event.detail > 0;
      focusedTrigger.current = event.detail === 0 && document.activeElement === event.currentTarget ? event.currentTarget : null;
      // Sample before React cancels the previous animation or replaces the
      // trigger, so rapid reversals start at the currently visible angle.
      const rotation = chevronRef.current ? getComputedStyle(chevronRef.current).rotate : undefined;
      fromRotation.current = rotation && rotation !== "none" ? rotation : open ? "90deg" : "0deg";
    },
  };
}
