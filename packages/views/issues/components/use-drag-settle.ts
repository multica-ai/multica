import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

/**
 * Content equality for the column map (`columnId -> ordered issue ids`). Two
 * maps are equal when they have the same column keys and each column's id list
 * matches element-for-element. Used to skip no-op `setColumns` writes.
 */
function columnsEqual(
  a: Record<string, string[]>,
  b: Record<string, string[]>,
): boolean {
  if (a === b) return true;
  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);
  if (aKeys.length !== bKeys.length) return false;
  for (const key of aKeys) {
    const av = a[key];
    const bv = b[key];
    if (av === bv) continue;
    if (!av || !bv || av.length !== bv.length) return false;
    for (let i = 0; i < av.length; i++) {
      if (av[i] !== bv[i]) return false;
    }
  }
  return true;
}

/**
 * Shared drag/settle state machine for the issue boards (board-view, list-view).
 *
 * All three drag surfaces (board, list, swimlane) follow the same contract:
 *
 *   - Local column state mirrors the TanStack Query cache *between* drags.
 *   - While dragging, or while a drop is *settling* (the move mutation is
 *     in flight), that mirror is frozen so an optimistic move isn't clobbered
 *     by a cache change that lands mid-flight.
 *   - On settle the lock releases and `settleVersion` bumps, forcing one resync
 *     from the now-reconciled cache.
 *
 * This hook owns that primitive so the surfaces can't drift apart (list-view
 * once silently lost the optimistic-move half of it). The resync `useEffect`
 * itself stays in each caller because its dependency list is data-source
 * specific (workspace board vs. status-only list), but it reads `settleVersion`
 * and the refs from here.
 *
 * `initialColumns` is only read once (useState initializer); callers drive
 * subsequent updates through their own resync effect + `setColumns`.
 */
export function useDragSettle(
  initialColumns: () => Record<string, string[]>,
) {
  const isDraggingRef = useRef(false);
  const isSettlingRef = useRef(false);
  // Throttles onDragOver: set true after a local move, cleared one frame later.
  const recentlyMovedRef = useRef(false);
  const [settleVersion, setSettleVersion] = useState(0);

  const [columns, setColumnsState] = useState<Record<string, string[]>>(
    initialColumns,
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

  // Equality-guarded column setter. When a resync (or a no-op drag) produces a
  // column map whose contents match the current one, return the SAME reference
  // so React bails out of the re-render. Second line of defense against the
  // cold-load update loop (MUL-4985): even if some input to `buildColumns`
  // regains a per-render-unstable identity, a content-equal rebuild no longer
  // forces a new state object and cannot spin the resync effect. It never
  // changes the resulting value — only skips redundant writes — so drag/settle
  // semantics are unchanged.
  const setColumns = useCallback<
    Dispatch<SetStateAction<Record<string, string[]>>>
  >((update) => {
    setColumnsState((prev) => {
      const next =
        typeof update === "function"
          ? (update as (p: Record<string, string[]>) => Record<string, string[]>)(
              prev,
            )
          : update;
      return columnsEqual(prev, next) ? prev : next;
    });
  }, []);

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [columns]);

  /**
   * Engage the settle lock and return the `onSettled` callback to hand to the
   * move mutation. The callback releases the lock and triggers a single resync.
   */
  const beginSettle = useCallback(() => {
    isSettlingRef.current = true;
    return () => {
      isSettlingRef.current = false;
      setSettleVersion((v) => v + 1);
    };
  }, []);

  return {
    columns,
    setColumns,
    columnsRef,
    isDraggingRef,
    isSettlingRef,
    recentlyMovedRef,
    settleVersion,
    beginSettle,
  };
}
