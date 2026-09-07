import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from "react";
import type { AgentTask } from "@multica/core/types";

const EMPTY_IDS: ReadonlySet<string> = new Set();
const EASING = "cubic-bezier(0.22, 1, 0.36, 1)";

function reveal(element: HTMLElement | null, duration: number, translate = 0) {
  if (!element?.animate) return;
  const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  return element.animate(
    translate && !reduced
      ? [{ opacity: 0, transform: `translateY(${translate}px)` }, { opacity: 1, transform: "translateY(0)" }]
      : [{ opacity: 0 }, { opacity: 1 }],
    { duration: reduced ? 60 : duration, easing: EASING },
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

/** Animate observed changes only. A virtualized row's first mount is always still. */
export function useRunCommentMotion(entering: boolean, replyId: string | undefined, status: string) {
  const ref = useRef<HTMLDivElement>(null);
  const previous = useRef({ entering, replyId, status });
  useLayoutEffect(() => {
    const before = previous.current;
    previous.current = { entering, replyId, status };
    const animations: (Animation | undefined)[] = [];
    if (entering && !before.entering) animations.push(reveal(ref.current, 160, 4));
    if (replyId && replyId !== before.replyId) {
      const body = Array.from(ref.current?.querySelectorAll<HTMLElement>("[data-comment-content]") ?? [])
        .find((element) => element.dataset.commentContent === replyId);
      animations.push(reveal(body ?? null, 160));
    }
    if (status !== before.status) animations.push(reveal(ref.current?.querySelector("[data-run-status]") ?? null, 100));
    return () => { for (const animation of animations) animation?.cancel(); };
  }, [entering, replyId, status]);
  return ref;
}

/** Animate the toggle itself, including when historical runs mount a new trigger. */
export function useRunDisclosureMotion(open: boolean) {
  const contentRef = useRef<HTMLDivElement>(null);
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
    const duration = open ? 180 : 120;
    const arrow = !reduced ? chevronRef.current?.animate?.(
      [{ rotate: fromRotation.current }, { rotate: open ? "90deg" : "0deg" }],
      { duration, easing: EASING },
    ) : undefined;
    const content = open ? reveal(contentRef.current, duration, -4) : undefined;
    return () => { arrow?.cancel(); content?.cancel(); };
  }, [open]);
  return {
    contentRef,
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
