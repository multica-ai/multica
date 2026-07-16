"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import {
  type IssueViewState,
  mergeViewStatePersisted,
  viewStorePersistOptions,
  viewStoreSlice,
} from "./view-store";

export const ISSUE_SURFACE_VIEW_STORAGE_KEY = "multica_issue_surface_views";

type PersistedIssueViewState = ReturnType<
  ReturnType<typeof viewStorePersistOptions>["partialize"]
>;

interface IssueSurfaceViewEntry {
  state: PersistedIssueViewState;
  updatedAt: string;
}

interface IssueSurfaceViewRegistryState {
  surfaces: Record<string, IssueSurfaceViewEntry>;
  setSurfaceState: (surfaceKey: string, state: IssueViewState) => void;
  clearSurfaceState: (surfaceKey: string) => void;
  pruneSurfaceStates: (validSurfaceKeys: Iterable<string>) => void;
}

const basePersist = viewStorePersistOptions(ISSUE_SURFACE_VIEW_STORAGE_KEY);
const surfaceStores = new Map<string, StoreApi<IssueViewState>>();
const suppressSurfacePersist = new Set<string>();

function persistedIssueViewState(state: IssueViewState): PersistedIssueViewState {
  return basePersist.partialize(state);
}

const issueSurfaceViewRegistryStore = createStore<IssueSurfaceViewRegistryState>()(
  persist(
    (set) => ({
      surfaces: {},
      setSurfaceState: (surfaceKey, state) =>
        set((current) => ({
          surfaces: {
            ...current.surfaces,
            [surfaceKey]: {
              state: persistedIssueViewState(state),
              updatedAt: new Date().toISOString(),
            },
          },
        })),
      clearSurfaceState: (surfaceKey) =>
        set((current) => {
          if (!current.surfaces[surfaceKey]) return current;
          const { [surfaceKey]: _removed, ...surfaces } = current.surfaces;
          return { surfaces };
        }),
      pruneSurfaceStates: (validSurfaceKeys) =>
        set((current) => {
          const valid = new Set(validSurfaceKeys);
          const surfaces = Object.fromEntries(
            Object.entries(current.surfaces).filter(([key]) => valid.has(key)),
          );
          if (Object.keys(surfaces).length === Object.keys(current.surfaces).length) {
            return current;
          }
          return { surfaces };
        }),
    }),
    {
      name: ISSUE_SURFACE_VIEW_STORAGE_KEY,
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ surfaces: state.surfaces }),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<IssueSurfaceViewRegistryState>;
        return {
          ...current,
          surfaces: p.surfaces ?? {},
        };
      },
    },
  ),
);

function resetStoreFromRegistry(surfaceKey: string, store: StoreApi<IssueViewState>) {
  const persisted =
    issueSurfaceViewRegistryStore.getState().surfaces[surfaceKey]?.state;
  const defaults = viewStoreSlice(store.setState);
  suppressSurfacePersist.add(surfaceKey);
  try {
    store.setState(mergeViewStatePersisted(persisted, defaults), true);
  } finally {
    suppressSurfacePersist.delete(surfaceKey);
  }
}

function resetAllSurfaceStoresFromRegistry() {
  for (const [surfaceKey, store] of surfaceStores) {
    resetStoreFromRegistry(surfaceKey, store);
  }
}

registerForWorkspaceRehydration(() => {
  void Promise.resolve(issueSurfaceViewRegistryStore.persist.rehydrate()).then(
    resetAllSurfaceStoresFromRegistry,
  );
});

export function getIssueSurfaceViewStore(
  surfaceKey: string,
): StoreApi<IssueViewState> {
  const existing = surfaceStores.get(surfaceKey);
  if (existing) return existing;

  const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  resetStoreFromRegistry(surfaceKey, store);
  store.subscribe((state) => {
    if (suppressSurfacePersist.has(surfaceKey)) return;
    issueSurfaceViewRegistryStore.getState().setSurfaceState(surfaceKey, state);
  });
  surfaceStores.set(surfaceKey, store);
  return store;
}

export function clearIssueSurfaceViewState(surfaceKey: string) {
  issueSurfaceViewRegistryStore.getState().clearSurfaceState(surfaceKey);
  const store = surfaceStores.get(surfaceKey);
  if (store) resetStoreFromRegistry(surfaceKey, store);
}

export function pruneIssueSurfaceViewStates(validSurfaceKeys: Iterable<string>) {
  const valid = new Set(validSurfaceKeys);
  issueSurfaceViewRegistryStore.getState().pruneSurfaceStates(valid);
  for (const [surfaceKey, store] of surfaceStores) {
    if (!valid.has(surfaceKey)) resetStoreFromRegistry(surfaceKey, store);
  }
}

export function getIssueSurfaceViewStateRegistrySnapshot() {
  return issueSurfaceViewRegistryStore.getState().surfaces;
}
