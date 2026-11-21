"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import {
  DEFAULT_CARD_PROPERTIES,
  DEFAULT_HIDDEN_STATUSES,
  type IssueViewState,
  mergeViewStatePersisted,
  normalizeSortForGrouping,
  type SortDirection,
  type SortField,
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

export interface ProjectDefaultSort {
  sortBy: SortField;
  sortDirection: SortDirection;
}

interface IssueSurfaceViewRegistryState {
  surfaces: Record<string, IssueSurfaceViewEntry>;
  /** Sort a project surface starts with before the user configures it. */
  projectDefaultSort: ProjectDefaultSort | null;
  setSurfaceState: (surfaceKey: string, state: IssueViewState) => void;
  clearSurfaceState: (surfaceKey: string) => void;
  pruneSurfaceStates: (validSurfaceKeys: Iterable<string>) => void;
  setProjectDefaultSort: (sort: ProjectDefaultSort | null) => void;
}

const basePersist = viewStorePersistOptions(ISSUE_SURFACE_VIEW_STORAGE_KEY);
const surfaceStores = new Map<string, StoreApi<IssueViewState>>();
const suppressSurfacePersist = new Set<string>();

const SURFACE_DEFAULTS_VERSION = 1;

const isProjectSurfaceKey = (surfaceKey: string) => surfaceKey.startsWith("project:");

function withSort<T extends Pick<IssueViewState, "grouping" | "sortBy" | "sortDirection" | "sortDirections">>(
  state: T,
  sort: ProjectDefaultSort,
): T {
  const next = normalizeSortForGrouping(state.grouping, sort.sortBy, sort.sortDirection);
  return {
    ...state,
    ...next,
    sortDirections: { ...state.sortDirections, [next.sortBy]: next.sortDirection },
  };
}

function defaultsForSurface(
  surfaceKey: string,
  set: StoreApi<IssueViewState>["setState"],
): IssueViewState {
  const defaults = viewStoreSlice(set);
  if (!isProjectSurfaceKey(surfaceKey)) return defaults;
  const projectDefaults = {
    ...defaults,
    cardProperties: { ...defaults.cardProperties, project: false },
  };
  const sort = issueSurfaceViewRegistryStore.getState().projectDefaultSort;
  return sort ? withSort(projectDefaults, sort) : projectDefaults;
}

function sanitizeProjectDefaultSort(value: unknown): ProjectDefaultSort | null {
  if (!value || typeof value !== "object") return null;
  const { sortBy, sortDirection } = value as Partial<ProjectDefaultSort>;
  if (typeof sortBy !== "string" || (sortDirection !== "asc" && sortDirection !== "desc")) {
    return null;
  }
  return { sortBy: sortBy as SortField, sortDirection };
}

function isLegacyCardDefault(cardProperties: unknown): boolean {
  if (!cardProperties || typeof cardProperties !== "object") return true;
  return Object.keys(DEFAULT_CARD_PROPERTIES).every(
    (key) =>
      (cardProperties as Partial<Record<keyof typeof DEFAULT_CARD_PROPERTIES, boolean>>)[
        key as keyof typeof DEFAULT_CARD_PROPERTIES
      ] !== false,
  );
}

function migrateLegacySurfaceState(
  surfaceKey: string,
  state: PersistedIssueViewState,
): PersistedIssueViewState {
  // Saved-view definitions are explicit user-authored defaults; never rewrite
  // their local copy as though it were an untouched built-in surface.
  if (surfaceKey.startsWith("view:")) return state;

  const next = { ...state };
  if (
    (state.sortBy === undefined || state.sortBy === "position") &&
    (state.sortDirection === undefined || state.sortDirection === "asc")
  ) {
    next.sortBy = "updated_at";
    next.sortDirection = "desc";
  }
  if (isLegacyCardDefault(state.cardProperties)) {
    next.cardProperties = {
      ...DEFAULT_CARD_PROPERTIES,
      ...(surfaceKey.startsWith("project:") ? { project: false } : {}),
    };
  }
  const legacyHidden = (state as unknown as { hiddenStatusCategories?: string[] }).hiddenStatusCategories;
  if (
    (state.hiddenStatuses ?? legacyHidden) === undefined ||
    (state.hiddenStatuses ?? legacyHidden)?.length === 0
  ) {
    next.hiddenStatuses = [...DEFAULT_HIDDEN_STATUSES];
  }
  return next;
}

function persistedIssueViewState(state: IssueViewState): PersistedIssueViewState {
  return basePersist.partialize(state);
}

const issueSurfaceViewRegistryStore = createStore<IssueSurfaceViewRegistryState>()(
  persist(
    (set) => ({
      surfaces: {},
      projectDefaultSort: null,
      setProjectDefaultSort: (sort) => set({ projectDefaultSort: sort }),
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
      version: SURFACE_DEFAULTS_VERSION,
      migrate: (persisted, version) => {
        const current = (persisted ?? {}) as Partial<IssueSurfaceViewRegistryState>;
        if (version >= SURFACE_DEFAULTS_VERSION) return current;
        return {
          ...current,
          surfaces: Object.fromEntries(
            Object.entries(current.surfaces ?? {}).map(([surfaceKey, entry]) => [
              surfaceKey,
              {
                ...entry,
                state: migrateLegacySurfaceState(surfaceKey, entry.state),
              },
            ]),
          ),
        };
      },
      partialize: (state) => ({
        surfaces: state.surfaces,
        projectDefaultSort: state.projectDefaultSort,
      }),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<IssueSurfaceViewRegistryState>;
        return {
          ...current,
          surfaces: p.surfaces ?? {},
          projectDefaultSort: sanitizeProjectDefaultSort(p.projectDefaultSort),
        };
      },
    },
  ),
);

function resetStoreFromRegistry(surfaceKey: string, store: StoreApi<IssueViewState>) {
  const persisted =
    issueSurfaceViewRegistryStore.getState().surfaces[surfaceKey]?.state;
  const defaults = defaultsForSurface(surfaceKey, store.setState);
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

  const store = createStore<IssueViewState>()((set) =>
    defaultsForSurface(surfaceKey, set),
  );
  resetStoreFromRegistry(surfaceKey, store);
  store.subscribe((state) => {
    if (suppressSurfacePersist.has(surfaceKey)) return;
    issueSurfaceViewRegistryStore.getState().setSurfaceState(surfaceKey, state);
  });
  surfaceStores.set(surfaceKey, store);
  return store;
}

/**
 * First-open seeding for saved-view surfaces (`view:<id>` keys): apply the
 * view's stored definition ONLY when this user has no local state for the
 * surface yet. Later opens keep the user's own adjustments — the definition
 * is a default, not a sync source.
 */
export function seedIssueSurfaceViewState(
  surfaceKey: string,
  definition: unknown,
) {
  const hasLocal =
    issueSurfaceViewRegistryStore.getState().surfaces[surfaceKey] !== undefined;
  if (hasLocal) return;
  const store = getIssueSurfaceViewStore(surfaceKey);
  // The definition is a server-owned jsonb blob — run it through the same
  // sanitizer persisted snapshots use so a malformed field degrades to the
  // default instead of poisoning the store. setState triggers the persist
  // subscription, so the seed becomes the surface's local state exactly
  // like a user-made change would.
  const defaults = viewStoreSlice(store.setState);
  store.setState(mergeViewStatePersisted(definition, defaults), true);
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

/**
 * Make `sort` the starting sort of this workspace's project surfaces that have
 * no saved state of their own; a project the user already configured keeps
 * its state. Choosing the built-in default clears the setting.
 */
export function setProjectDefaultSort(sort: ProjectDefaultSort) {
  const builtin = viewStoreSlice(() => {});
  const isBuiltin =
    sort.sortBy === builtin.sortBy && sort.sortDirection === builtin.sortDirection;
  issueSurfaceViewRegistryStore.getState().setProjectDefaultSort(isBuiltin ? null : sort);
  const { surfaces } = issueSurfaceViewRegistryStore.getState();
  for (const [surfaceKey, store] of surfaceStores) {
    if (isProjectSurfaceKey(surfaceKey) && !surfaces[surfaceKey]) {
      resetStoreFromRegistry(surfaceKey, store);
    }
  }
}

export function getProjectDefaultSort(): ProjectDefaultSort | null {
  return issueSurfaceViewRegistryStore.getState().projectDefaultSort;
}

export function getIssueSurfaceViewStateRegistrySnapshot() {
  return issueSurfaceViewRegistryStore.getState().surfaces;
}
