"use client";

import { useEffect, useRef, type RefObject } from "react";
import type { VirtuosoHandle } from "react-virtuoso";

// ---------------------------------------------------------------------------
// Deep-link anchor calibration for the virtualized issue timeline (MUL-4812).
//
// Virtuoso puts the target row at the top of the viewport on the first frame
// (`initialTopMostItemIndex`), but that position is only as good as the
// heights known at that moment. Comment images carry no intrinsic size
// (`renderImage` in packages/views/common/markdown.tsx passes src/alt only),
// so anything above the target — the description, the sub-issues block, an
// earlier comment's image, a mermaid diagram — can grow *after* the anchor
// lands and push the target out of view.
//
// This hook holds the target in place until the user takes over.
//
// Wake-up sources (why three, and why the obvious two are not enough):
//
//   1. ResizeObserver on the scroll CONTENT WRAPPER. This is the load-bearing
//      one. `ResizeObserver` reports SIZE changes of the observed element, not
//      POSITION changes — so observing the target row alone never fires when
//      content *above* it grows and shoves it down. Observing the scroll
//      container is equally useless: it is a fixed-height `overflow-y-auto`
//      viewport, so late content grows its scrollHeight while its border-box
//      stays exactly the same and the observer never fires. The content
//      wrapper is the auto-height element that encloses title, description,
//      sub-issues, the timeline and the composer, so *any* growth above the
//      target changes its height and wakes us. One observer, no enumeration
//      of "things that might grow" that the next feature would silently miss.
//   2. ResizeObserver on the target row itself. Redundant with (1) for
//      correctness, but faster: when the target's own image lands, the wrapper
//      only changes after Virtuoso re-measures the row and updates its total
//      height — two hops. Observing the row reacts on the first.
//   3. A bounded low-frequency re-check. Covers the one case ResizeObserver
//      cannot see: content above growing while content below shrinks by the
//      same amount in the same frame — net wrapper height unchanged, target
//      still moved.
//
// All three funnel into the same throttled correction, so there is exactly one
// place that decides what to do and the backstop cannot introduce a second
// correction path.
//
// Corrections are deliberately split by magnitude:
//   - more than a viewport off  -> hand it back to Virtuoso via scrollToIndex.
//     The row is likely unmounted; only Virtuoso can materialize and land on it.
//   - within a viewport         -> nudge scrollTop directly. Cheap, exact, and
//     the resulting `scroll` event makes Virtuoso recompute the `offsetTop` it
//     caches for customScrollParent (see useWindowViewportRect upstream),
//     which is itself stale for exactly the same reason we are here.
//   - under the dead zone       -> do nothing. Sub-pixel churn is invisible and
//     re-entering the correction path is how oscillation starts.
// ---------------------------------------------------------------------------

/** Deviation we treat as "landed". Below this, correcting is pure churn. */
const DEAD_ZONE_PX = 4;
/** Hard stop, whichever comes first with the user's own scroll input. */
const MAX_DURATION_MS = 5000;
/** Backstop poll interval — see wake-up source (3). */
const RECHECK_INTERVAL_MS = 250;

type UseCommentAnchorCalibrationArgs = {
  /** Deep-link target comment id (root or reply), or null when not deep-linked. */
  targetCommentId: string | null;
  /** Flat index of the target's top-level row in the Virtuoso data, or -1. */
  targetIndex: number;
  /** The `overflow-y-auto` viewport. */
  scrollContainerEl: HTMLElement | null;
  /** Auto-height wrapper inside the viewport — wake-up source (1). */
  contentWrapperEl: HTMLElement | null;
  virtuosoRef: RefObject<VirtuosoHandle | null>;
  /** Breathing room to leave above the target's header. */
  topGap: number;
  enabled: boolean;
};

export function useCommentAnchorCalibration({
  targetCommentId,
  targetIndex,
  scrollContainerEl,
  contentWrapperEl,
  virtuosoRef,
  topGap,
  enabled,
}: UseCommentAnchorCalibrationArgs) {
  // Set once the user takes control of the scroll, and never cleared for the
  // life of the mount. Two callers depend on it:
  //   - calibration stops immediately (never fight the user's scroll);
  //   - a passively-changed target does not re-anchor (see the effect below).
  const userScrolledRef = useRef(false);
  // `undefined` = the effect has not run yet. Distinguishes "mounted with a
  // target" (Virtuoso's initialTopMostItemIndex owns that first landing) from
  // "target changed while mounted" (only scrollToIndex can move it, because
  // initialTopMostItemIndex is mount-only).
  const previousTargetRef = useRef<string | null | undefined>(undefined);
  // Absolute per-target deadline, so a timeline that keeps changing height
  // re-runs the effect without ever extending the session.
  const deadlineRef = useRef<{ at: number; id: string } | null>(null);
  const targetIndexRef = useRef(targetIndex);
  targetIndexRef.current = targetIndex;

  useEffect(() => {
    if (!scrollContainerEl) return;
    // Only genuine user input counts. `scroll` events are excluded on purpose:
    // our own corrections emit them, and treating those as "the user scrolled"
    // would make the hook cancel itself on its first correction.
    //
    // Any keydown counts, not just the scroll keys: if the user is typing in
    // the composer, moving the timeline under them is exactly as unwelcome as
    // moving it while they page down.
    const takeOver = () => {
      userScrolledRef.current = true;
    };
    const opts = { passive: true } as const;
    scrollContainerEl.addEventListener("wheel", takeOver, opts);
    scrollContainerEl.addEventListener("touchstart", takeOver, opts);
    window.addEventListener("keydown", takeOver, opts);
    return () => {
      scrollContainerEl.removeEventListener("wheel", takeOver);
      scrollContainerEl.removeEventListener("touchstart", takeOver);
      window.removeEventListener("keydown", takeOver);
    };
  }, [scrollContainerEl]);

  useEffect(() => {
    const previousTarget = previousTargetRef.current;
    const isFirstRun = previousTarget === undefined;
    previousTargetRef.current = targetCommentId;

    if (!enabled || !targetCommentId || !scrollContainerEl) return;

    // Once the user has taken over there is nothing to do: no re-anchor (that
    // would yank the viewport away from what they are reading) and no
    // calibration (same reason). This is the whole latch — see the ruling in
    // MUL-4812: a target that changes while mounted is, inside the inbox,
    // necessarily a passive update. IssueDetail is keyed by issue_id and
    // deduplicateInboxItems keeps exactly one row per issue, so hand-picking a
    // different comment for the same issue is not reachable; the change came
    // from a refreshed inbox query swapping in a newer notification.
    if (userScrolledRef.current) return;

    // Mount-time targets are landed by Virtuoso's initialTopMostItemIndex.
    // Anything later needs scrollToIndex — that prop is read only at mount.
    const isRetarget = !isFirstRun && previousTarget !== targetCommentId;

    if (deadlineRef.current?.id !== targetCommentId) {
      deadlineRef.current = { at: Date.now() + MAX_DURATION_MS, id: targetCommentId };
    }
    const deadline = deadlineRef.current;
    if (Date.now() >= deadline.at) return;

    if (isRetarget && targetIndexRef.current >= 0) {
      virtuosoRef.current?.scrollToIndex({
        align: "start",
        index: targetIndexRef.current,
        offset: -topGap,
      });
    }

    let frame = 0;
    let poll: ReturnType<typeof setInterval> | undefined;
    let stopped = false;

    const stop = () => {
      stopped = true;
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
      if (poll) clearInterval(poll);
      poll = undefined;
    };

    const correct = () => {
      frame = 0;
      if (stopped || userScrolledRef.current) return stop();
      if (Date.now() >= deadline.at) return stop();

      const index = targetIndexRef.current;
      const target = document.getElementById(`comment-${targetCommentId}`);
      if (!target) {
        // Virtualized away: it can only be far outside the viewport, and only
        // Virtuoso can bring it back.
        if (index >= 0) {
          virtuosoRef.current?.scrollToIndex({ align: "start", index, offset: -topGap });
        }
        return;
      }

      const containerRect = scrollContainerEl.getBoundingClientRect();
      const targetRect = target.getBoundingClientRect();
      const deviation = targetRect.top - (containerRect.top + topGap);
      if (Math.abs(deviation) < DEAD_ZONE_PX) return;

      if (Math.abs(deviation) > scrollContainerEl.clientHeight && index >= 0) {
        virtuosoRef.current?.scrollToIndex({ align: "start", index, offset: -topGap });
        return;
      }
      scrollContainerEl.scrollTop += deviation;
    };

    // One correction per frame at most, no matter how many sources fired.
    // Virtuoso settles estimated heights into real ones across frames; landing
    // a second write inside the same frame is what makes the two loops ring.
    const schedule = () => {
      if (stopped || frame) return;
      frame = requestAnimationFrame(correct);
    };

    const wrapperObserver = new ResizeObserver(schedule);
    if (contentWrapperEl) wrapperObserver.observe(contentWrapperEl);

    // The row is re-created as Virtuoso mounts/unmounts it, so re-resolve the
    // node on every poll rather than observing once and holding a dead ref.
    const rowObserver = new ResizeObserver(schedule);
    let observedRow: HTMLElement | null = null;
    const rebindRow = () => {
      const row = document.getElementById(`comment-${targetCommentId}`);
      if (row === observedRow) return;
      if (observedRow) rowObserver.unobserve(observedRow);
      if (row) rowObserver.observe(row);
      observedRow = row;
    };
    rebindRow();

    poll = setInterval(() => {
      if (stopped || userScrolledRef.current || Date.now() >= deadline.at) return stop();
      rebindRow();
      schedule();
    }, RECHECK_INTERVAL_MS);

    const deadlineTimer = setTimeout(stop, Math.max(0, deadline.at - Date.now()));

    schedule();

    return () => {
      stop();
      clearTimeout(deadlineTimer);
      wrapperObserver.disconnect();
      rowObserver.disconnect();
    };
  }, [targetCommentId, targetIndex, enabled, scrollContainerEl, contentWrapperEl, virtuosoRef, topGap]);
}
