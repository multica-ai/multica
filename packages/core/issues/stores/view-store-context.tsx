"use client";

import { createContext, use } from "react";
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
  const store = use(ViewStoreContext);
  if (!store)
    throw new Error("useViewStore must be used within ViewStoreProvider");
  return useStore(store, selector);
}

export function useViewStoreApi(): StoreApi<IssueViewState> {
  const store = use(ViewStoreContext);
  if (!store)
    throw new Error("useViewStoreApi must be used within ViewStoreProvider");
  return store;
}

/**
 * Resolves the per-surface view store an IssueSurface should use. A platform
 * injects this to override where view state lives — the desktop tab shell
 * supplies per-tab, session-backed stores so two tabs on the same path keep
 * independent filters. Web mounts no provider, so IssueSurface falls back to
 * the global surface registry (`getIssueSurfaceViewStore`).
 */
export type IssueViewStoreFactory = (
  surfaceKey: string,
) => StoreApi<IssueViewState>;

const IssueViewStoreFactoryContext = createContext<IssueViewStoreFactory | null>(
  null,
);

export function IssueViewStoreFactoryProvider({
  factory,
  children,
}: {
  factory: IssueViewStoreFactory;
  children: React.ReactNode;
}) {
  return (
    <IssueViewStoreFactoryContext.Provider value={factory}>
      {children}
    </IssueViewStoreFactoryContext.Provider>
  );
}

/** The injected view-store factory, or null when no platform provides one. */
export function useIssueViewStoreFactory(): IssueViewStoreFactory | null {
  return use(IssueViewStoreFactoryContext);
}
