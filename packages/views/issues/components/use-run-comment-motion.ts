import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from "react";
import { prefersReducedMotion } from "@multica/core/appearance";
import type { AgentTask } from "@multica/core/types";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

const EMPTY_IDS: ReadonlySet<string> = new Set();

export function revealElement(element: HTMLElement | null, duration: number, translate = 0) {
  if (!element?.animate) return;
  if (prefersReducedMotion()) return;
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

/**
 * How long an arrival may wait for its row to mount. A row the virtual list
 * mounts later, as the reader scrolls to it, is history by then and stays still.
 */
const ARRIVAL_REVEAL_WINDOW_MS = 1000;

export interface CommentArrivals {
  /** True once per comment that arrived after the first load, if it mounts soon enough. */
  take(commentId: string): boolean;
}

export const CommentArrivalsContext = createContext<CommentArrivals | null>(null);

/**
 * Track comments (roots and replies) added after the timeline's first load:
 * a live comment from someone else, an agent's reply, or one the reader posted.
 * Recorded during render, so a row mounting in the same commit can claim it.
 * Entries that come with a reload (a thread fetched for a jump) are history.
 */
export function useCommentArrivals(
  issueId: string,
  entries: readonly { id: string; type: string }[],
  ready: boolean,
): CommentArrivals {
  const known = useRef<{ issueId: string; ids: Set<string>; reloading: boolean } | null>(null);
  const pending = useRef(new Map<string, number>());
  if (!ready) {
    if (known.current?.issueId === issueId) known.current.reloading = true;
  } else if (!known.current || known.current.issueId !== issueId) {
    known.current = { issueId, ids: new Set(entries.map((entry) => entry.id)), reloading: false };
    pending.current.clear();
  } else {
    const live = !known.current.reloading;
    known.current.reloading = false;
    for (const entry of entries) {
      if (known.current.ids.has(entry.id)) continue;
      known.current.ids.add(entry.id);
      if (live && entry.type === "comment") pending.current.set(entry.id, performance.now());
    }
  }
  const [arrivals] = useState<CommentArrivals>(() => ({
    take(commentId) {
      const at = pending.current.get(commentId);
      if (at === undefined) return false;
      pending.current.delete(commentId);
      return performance.now() - at < ARRIVAL_REVEAL_WINDOW_MS;
    },
  }));
  return arrivals;
}

/**
 * Fade a comment row in when it is a live arrival. Not cancelled on unmount:
 * a strict-mode effect replay must not stop the one reveal the row gets.
 */
export function useCommentArrivalReveal<T extends HTMLElement>(commentId: string) {
  const arrivals = useContext(CommentArrivalsContext);
  const ref = useRef<T>(null);
  useLayoutEffect(() => {
    if (!arrivals?.take(commentId)) return;
    revealElement(ref.current, UI_MOTION_DURATION.fast * 1000, 4);
  }, [arrivals, commentId]);
  return ref;
}

/** Pause ambient run motion outside the viewport, including Virtuoso overscan. */
export function useRunAnimationVisibility<T extends Element>() {
  const [element, setElement] = useState<T | null>(null);
  const [visible, setVisible] = useState(true);
  const ref = useCallback((node: T | null) => setElement(node), []);
  useEffect(() => {
    if (!element || typeof IntersectionObserver !== "function") return;
    // One notification can queue several changes, oldest first: a keyed move
    // reports the detached row, then its new place. Only the last is current.
    const observer = new IntersectionObserver((entries) => setVisible(entries.at(-1)?.isIntersecting ?? true));
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
      animations.push(revealElement(ref.current, UI_MOTION_DURATION.fast * 1000, 4));
    } else if (replyId && replyId !== before.replyId) {
      const body = Array.from(ref.current?.querySelectorAll<HTMLElement>("[data-comment-content]") ?? [])
        .find((element) => element.dataset.commentContent === replyId);
      animations.push(revealElement(body ?? null, UI_MOTION_DURATION.fast * 1000));
    } else if (status !== before.status) {
      animations.push(revealElement(ref.current?.querySelector("[data-run-status]") ?? null, UI_MOTION_DURATION.micro * 1000));
    }
    return () => { for (const animation of animations) animation?.cancel(); };
  }, [entering, replyId, status]);
  return ref;
}

/** Animate the toggle itself, including when historical runs mount a new trigger. */
export function useRunDisclosureMotion(open: boolean) {
  const chevronRef = useRef<SVGSVGElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
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
    if (prefersReducedMotion()) return;
    const duration = (open ? UI_MOTION_DURATION.fast : UI_MOTION_DURATION.micro) * 1000;
    const arrow = chevronRef.current?.animate?.(
      [{ rotate: fromRotation.current }, { rotate: open ? "90deg" : "0deg" }],
      { duration, easing: UI_EASE_OUT_CSS },
    );
    const content = open ? revealElement(contentRef.current, UI_MOTION_DURATION.fast * 1000, -3) : undefined;
    return () => {
      arrow?.cancel();
      content?.cancel();
    };
  }, [open]);
  return {
    chevronRef,
    contentRef,
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
