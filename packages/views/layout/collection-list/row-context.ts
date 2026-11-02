"use client";

import { createContext, useContext } from "react";

export interface RowMenuState {
  open: boolean;
  point: { x: number; y: number } | null;
}

export interface RowContextValue {
  menu: RowMenuState;
  setMenu: (next: RowMenuState) => void;
}

export const CollectionRowContext = createContext<RowContextValue | null>(null);

export function useCollectionRowMenuState(): RowContextValue | null {
  return useContext(CollectionRowContext);
}
