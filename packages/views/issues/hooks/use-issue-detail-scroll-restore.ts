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

export function useIssueDetailScrollRestore({
  restoreKey,
  scrollContainerEl,
  ready,
  disabled = false,
  overrideTop,
}: UseIssueDetailScrollRestoreArgs) {
  const restoredKeyRef = useRef<string | null>(null);

  useLayoutEffect(() => {
    restoredKeyRef.current = null;
  }, [restoreKey]);

  useLayoutEffect(() => {
    if (!scrollContainerEl || disabled || !ready) return;

    const save = () => {
      saveScrollPosition(restoreKey, scrollContainerEl.scrollTop);
    };

    scrollContainerEl.addEventListener("scroll", save, { passive: true });

    return () => {
      save();
      scrollContainerEl.removeEventListener("scroll", save);
    };
  }, [scrollContainerEl, restoreKey, ready, disabled]);

  useLayoutEffect(() => {
    if (!scrollContainerEl || !ready) return;
    if (disabled) {
      restoredKeyRef.current = restoreKey;
      return;
    }
    if (restoredKeyRef.current === restoreKey) return;

    restoredKeyRef.current = restoreKey;

    const target = overrideTop ?? scrollPositions.get(restoreKey) ?? 0;
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
  const requiredStableFrames = 2;

  el.scrollTop = target;

  let frameId: number;

  const tick = () => {
    if (cancelled || !el.isConnected) return;

    attempts += 1;

    if (Math.abs(el.scrollTop - target) <= 1) {
      stableFrames += 1;
    } else {
      stableFrames = 0;
      el.scrollTop = target;
    }

    // A virtualized timeline initializes after the parent's layout effect and
    // may reset a synchronous scroll write. Requiring stability across frames
    // keeps the restore alive long enough to outlast that initialization.
    if (stableFrames >= requiredStableFrames || attempts >= maxAttempts) {
      return;
    }

    frameId = requestAnimationFrame(tick);
  };

  frameId = requestAnimationFrame(tick);

  return () => {
    cancelled = true;
    cancelAnimationFrame(frameId);
  };
}
