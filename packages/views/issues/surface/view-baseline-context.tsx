"use client";

import { createContext, use, type ReactNode } from "react";
import type { IssueViewBaseline } from "@multica/core/issue-views/baseline";

/**
 * The open saved view's baseline for everything living inside an
 * IssueSurface — board column menus, the filtered-empty state, swimlanes.
 * `null` = no view open. Kept as its own context so deep consumers don't
 * need the active-view hook (and its list query) re-run per component.
 */
const ViewBaselineContext = createContext<IssueViewBaseline | null>(null);

export function ViewBaselineProvider({
  baseline,
  children,
}: {
  baseline: IssueViewBaseline | null;
  children: ReactNode;
}) {
  return (
    <ViewBaselineContext.Provider value={baseline}>
      {children}
    </ViewBaselineContext.Provider>
  );
}

export function useViewBaseline(): IssueViewBaseline | null {
  return use(ViewBaselineContext);
}
