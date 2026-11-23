"use client";

import { createContext, useContext } from "react";
import type { RowMenuState } from "../../common/point-menu";

export type { RowMenuState };

export interface RowContextValue {
  menu: RowMenuState;
  setMenu: (next: RowMenuState) => void;
}

/** Shares one row's menu state between the row's right-click handler and the
 *  CollectionRowMenu inside it. CollectionList provides it for its rows;
 *  other rows provide it with `useRowMenu`. */
export const CollectionRowContext = createContext<RowContextValue | null>(null);

export function useCollectionRowMenuState(): RowContextValue | null {
  return useContext(CollectionRowContext);
}
