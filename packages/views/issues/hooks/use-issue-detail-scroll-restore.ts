import { useLayoutEffect, useRef } from "react";

type UseIssueDetailScrollRestoreArgs = {
  restoreKey: string;
  scrollContainerEl: HTMLElement | null;
  ready: boolean;
  disabled?: boolean;
  /**
   * Authoritative restore target from the platform's tab memento, when one
   * is being served (MUL-4741). It wins over this hook's module-level map:
   * the memento is captured from the live DOM at the moment the view is
   * left, while the map only hears scroll *events* — content-driven
   * position shifts (scroll anchoring, streaming blocks collapsing) move
   * scrollTop without one, leaving the map holding an older visit. Without
   * this, the two restores race and the retry loop below overwrites the
   * memento's fresher offset with the stale one.
   */
  overrideTop?: number;
};

const scrollPositions = new Map<string, number>();
const SCROLL_POSITION_CACHE_MAX_SIZE = 100;

/**
 * Events that prove the scroll point belongs to the user. The restore loop
 * cannot tell a layout drift from a user scroll by comparing scrollTop, so
 * any of these hands the scroll point back to the user immediately.
 */
const USER_INTENT_EVENTS = [
  "wheel",
  "touchstart",
  "pointerdown",
  "keydown",
] as const;

export function useIssueDetailScrollRestore({
  restoreKey,
  scrollContainerEl,
  ready,
  disabled = false,
  overrideTop,
}: UseIssueDetailScrollRestoreArgs) {
  const appliedTargetRef = useRef<number | null>(null);

  useLayoutEffect(() => {
    appliedTargetRef.current = null;
  }, [restoreKey]);

  useLayoutEffect(() => {
    if (!scrollContainerEl || disabled || !ready) return;

    const save = () => {
      const top = scrollContainerEl.scrollTop;
      // A retained, off-screen sibling surface (the route keeps the last
      // detail mounted while another one is on screen) reports scrollTop 0
      // with no layout box — display:none zeroes both. That is not a position
      // the user left the issue at, and recording it would erase the offset
      // the next visit restores. A container that really is scrolled back to
      // the top still has its height, so it still clears its entry.
      if (top <= 0 && scrollContainerEl.scrollHeight === 0) return;
      saveScrollPosition(restoreKey, top);
    };

    scrollContainerEl.addEventListener("scroll", save, { passive: true });

    return () => {
      save();
      scrollContainerEl.removeEventListener("scroll", save);
    };
  }, [scrollContainerEl, restoreKey, ready, disabled]);

  useLayoutEffect(() => {
    if (!scrollContainerEl || !ready) return;

    const target = overrideTop ?? scrollPositions.get(restoreKey) ?? 0;

    if (disabled) {
      // The comment deep-link jump owns the scroll. Record what a later run
      // would apply so that run does not yank the jumped position away.
      appliedTargetRef.current = target;
      return;
    }
    // The memento is read from the pathname, and the route's canonical-URL
    // rewrite (/issues/<uuid> -> /issues/KEY-1) lands a commit after mount, so
    // the first run can only apply this hook's own map. Re-applying whenever
    // the target changes lets that authoritative offset win instead of being
    // swallowed by a once-per-visit latch.
    if (appliedTargetRef.current === target) return;
    appliedTargetRef.current = target;

    if (target <= 1) {
      scrollContainerEl.scrollTop = target;
      return;
    }

    return restoreScrollTopWithRetry(scrollContainerEl, target);
  }, [scrollContainerEl, restoreKey, ready, disabled, overrideTop]);
}

function saveScrollPosition(restoreKey: string, scrollTop: number) {
  if (scrollPositions.has(restoreKey)) {
    scrollPositions.delete(restoreKey);
  } else if (scrollPositions.size >= SCROLL_POSITION_CACHE_MAX_SIZE) {
    const oldestKey = scrollPositions.keys().next().value;
    if (oldestKey !== undefined) scrollPositions.delete(oldestKey);
  }

  scrollPositions.set(restoreKey, scrollTop);
}

function restoreScrollTopWithRetry(el: HTMLElement, target: number) {
  let cancelled = false;
  let attempts = 0;
  let stableFrames = 0;
  const maxAttempts = 30;
  let frameId = 0;

  el.scrollTop = target;

  const stop = () => {
    if (cancelled) return;
    cancelled = true;
    for (const type of USER_INTENT_EVENTS) {
      el.removeEventListener(type, stop);
    }
    cancelAnimationFrame(frameId);
  };

  for (const type of USER_INTENT_EVENTS) {
    el.addEventListener(type, stop, { passive: true, once: true });
  }

  const tick = () => {
    if (cancelled || !el.isConnected) return;

    attempts += 1;

    if (Math.abs(el.scrollTop - target) <= 1) {
      stableFrames += 1;
    } else {
      stableFrames = 0;
      el.scrollTop = target;
    }

    // Attachment metadata can replace an image URL a few frames after the
    // first decode, and both passes move scrollTop. Wait for the images
    // themselves instead of for a fixed frame count: the restore then stays
    // alive exactly as long as the layout can still move, and releases on
    // the first frame after the last image settles.
    if ((stableFrames >= 1 && !hasPendingImage(el)) || attempts >= maxAttempts) {
      stop();
      return;
    }

    frameId = requestAnimationFrame(tick);
  };

  frameId = requestAnimationFrame(tick);

  return stop;
}

function hasPendingImage(el: HTMLElement) {
  for (const img of el.querySelectorAll("img")) {
    if (!img.complete) return true;
  }
  return false;
}
