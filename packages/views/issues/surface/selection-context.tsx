"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";

export interface IssueSurfaceSelection {
  selectedIds: Set<string>;
  toggle: (id: string) => void;
  select: (ids: string[]) => void;
  deselect: (ids: string[]) => void;
  clear: () => void;
}

const IssueSurfaceSelectionContext =
  createContext<IssueSurfaceSelection | null>(null);

export function useCreateIssueSurfaceSelection(
  surfaceKey: string,
  resetKey = surfaceKey,
): IssueSurfaceSelection {
  const [selectedIds, setSelectedIds] = useState(() => new Set<string>());
  const [committedResetKey, setCommittedResetKey] = useState(resetKey);

  // Render-phase reset (the React "adjusting state when props change"
  // pattern), NOT an effect: an effect runs after commit, so one frame would
  // ship the NEW membership (filters/search) with the OLD selection — batch
  // consumers could act on rows the window no longer contains during that
  // gap. Setting state during render re-renders synchronously before commit,
  // so no such frame ever becomes visible. The size guard keeps the common
  // nothing-selected key change from allocating a new Set and re-rendering
  // the surface.
  if (committedResetKey !== resetKey) {
    setCommittedResetKey(resetKey);
    if (selectedIds.size > 0) setSelectedIds(new Set());
  }

  const toggle = useCallback((id: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const select = useCallback((ids: string[]) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      for (const id of ids) next.add(id);
      return next;
    });
  }, []);

  const deselect = useCallback((ids: string[]) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      for (const id of ids) next.delete(id);
      return next;
    });
  }, []);

  const clear = useCallback(() => {
    setSelectedIds(new Set());
  }, []);

  return useMemo(
    () => ({ selectedIds, toggle, select, deselect, clear }),
    [clear, deselect, select, selectedIds, toggle],
  );
}

export function IssueSurfaceSelectionProvider({
  selection,
  children,
}: {
  selection: IssueSurfaceSelection;
  children: ReactNode;
}) {
  return (
    <IssueSurfaceSelectionContext.Provider value={selection}>
      {children}
    </IssueSurfaceSelectionContext.Provider>
  );
}

export function useIssueSurfaceSelectionOptional() {
  return useContext(IssueSurfaceSelectionContext);
}

export function useIssueSurfaceSelection(): IssueSurfaceSelection {
  const surfaceSelection = useIssueSurfaceSelectionOptional();
  const globalSelectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const globalToggle = useIssueSelectionStore((s) => s.toggle);
  const globalSelect = useIssueSelectionStore((s) => s.select);
  const globalDeselect = useIssueSelectionStore((s) => s.deselect);
  const globalClear = useIssueSelectionStore((s) => s.clear);

  return (
    surfaceSelection ?? {
      selectedIds: globalSelectedIds,
      toggle: globalToggle,
      select: globalSelect,
      deselect: globalDeselect,
      clear: globalClear,
    }
  );
}
