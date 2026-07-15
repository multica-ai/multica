import { useLayoutEffect, type RefObject } from "react";
import { useTabStore, getActiveTab } from "@/stores/tab-store";

/**
 * Restore a tab session's captured scroll positions after the ActiveTabHost
 * (re)mounts (MUL-4741 state-restoration protocol).
 *
 * Runs in a layout effect — after the subtree committed, before paint — so a
 * warm restore (React Query cache still populated, content at full height on
 * the first commit) is a single synchronous scrollTop assignment and the
 * first painted frame is already at the right offset.
 *
 * Cold restores (data gc'd, skeletons committed first) and virtualized lists
 * mid-hydration have a scrollHeight smaller than the saved offset, so a bare
 * assignment would be clamped to 0. The memento saved the container's
 * scrollHeight at capture time: a temporary hidden spacer pre-sizes the
 * container to that height, making the assignment stick immediately; a rAF
 * loop then waits for the real content to reach the saved height (data
 * arriving, lists hydrating) and removes the spacer. The spacer is a foreign
 * child inside a React-managed container — tolerated because its lifetime is
 * bounded and fully owned here.
 */
export function useTabMementoRestore(
  tabId: string,
  hostRef: RefObject<HTMLElement | null>,
): void {
  useLayoutEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const active = getActiveTab(useTabStore.getState());
    if (!active || active.id !== tabId) return;
    const entries = Object.entries(active.memento.scroll);
    if (entries.length === 0) return;

    const containers = Array.from(
      host.querySelectorAll<HTMLElement>("[data-tab-scroll-root]"),
    );
    const cleanups: Array<() => void> = [];
    for (const [key, saved] of entries) {
      const el = containers.find(
        (c) => (c.getAttribute("data-tab-scroll-root") || "main") === key,
      );
      if (!el) continue;
      cleanups.push(restoreScroll(el, saved));
    }
    return () => {
      for (const cleanup of cleanups) cleanup();
    };
  }, [tabId, hostRef]);
}

// ~10s at 60fps — generous enough to cover a cold restore's refetch without
// keeping a zombie loop alive forever if the saved height is unreachable
// (list shrank server-side).
const MAX_WAIT_FRAMES = 600;

function restoreScroll(
  el: HTMLElement,
  saved: { top: number; height: number },
): () => void {
  el.scrollTop = saved.top;
  if (el.scrollTop === saved.top) return () => {};

  const deficit = Math.max(0, saved.height - el.scrollHeight);
  const spacer = document.createElement("div");
  spacer.style.height = `${deficit}px`;
  spacer.style.visibility = "hidden";
  spacer.setAttribute("data-tab-scroll-spacer", "");
  el.appendChild(spacer);
  el.scrollTop = saved.top;

  let cancelled = false;
  let frames = 0;
  const tick = () => {
    if (cancelled) return;
    const realHeight = el.scrollHeight - spacer.offsetHeight;
    if (realHeight >= saved.height || frames >= MAX_WAIT_FRAMES) {
      spacer.remove();
      // If the content ended up shorter than at capture time the browser
      // clamps this naturally — that's the correct outcome.
      el.scrollTop = saved.top;
      return;
    }
    frames++;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);

  return () => {
    cancelled = true;
    spacer.remove();
  };
}
