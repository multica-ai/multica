import { useCallback, useEffect, useRef, useState } from "react";

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

  const [columns, setColumns] = useState<Record<string, string[]>>(
    initialColumns,
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

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
