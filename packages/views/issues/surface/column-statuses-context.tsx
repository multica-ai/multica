"use client";

import { createContext, use, type ReactNode } from "react";
import type { IssueStatus } from "@multica/core/types";

/**
 * The status columns the surface's Display menu can show or hide, in column
 * order. Each surface page renders its own header, so the list travels by
 * context instead of through every header's props. `null` outside a surface.
 */
const ColumnStatusesContext = createContext<IssueStatus[] | null>(null);

export function ColumnStatusesProvider({
  statuses,
  children,
}: {
  statuses: IssueStatus[];
  children: ReactNode;
}) {
  return (
    <ColumnStatusesContext.Provider value={statuses}>
      {children}
    </ColumnStatusesContext.Provider>
  );
}

export function useColumnStatuses(): IssueStatus[] | null {
  return use(ColumnStatusesContext);
}
