"use client";

import { createContext, useContext } from "react";
import { useStore, type StoreApi } from "zustand";
import type { IssueViewState } from "./view-store";

const ViewStoreContext = createContext<StoreApi<IssueViewState> | null>(null);

export function ViewStoreProvider({
  store,
  children,
}: {
  store: StoreApi<IssueViewState>;
  children: React.ReactNode;
}) {
  return (
    <ViewStoreContext.Provider value={store}>
      {children}
    </ViewStoreContext.Provider>
  );
}

export function useViewStore<T>(selector: (state: IssueViewState) => T): T {
  const store = useContext(ViewStoreContext);
  if (!store)
    throw new Error("useViewStore must be used within ViewStoreProvider");
  return useStore(store, selector);
}

export function useViewStoreApi(): StoreApi<IssueViewState> {
  const store = useContext(ViewStoreContext);
  if (!store)
    throw new Error("useViewStoreApi must be used within ViewStoreProvider");
  return store;
}
