"use client";

import { create, type StoreApi } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

// Shared view preferences for the collection list pages (agents, skills,
// autopilots, projects, squads): sort, column visibility and multi-select
// filters, persisted per workspace. Row selection stays session-local.

export type CollectionSortDirection = "asc" | "desc";

export type CollectionFilters<F> = { [K in keyof F]: string[] };

export interface CollectionViewCore<
  Sort extends string,
  Column extends string,
  Filters extends CollectionFilters<Filters>,
> {
  sortField: Sort;
  sortDirection: CollectionSortDirection;
  hiddenColumns: Column[];
  filters: Filters;
  /** Header click: toggles direction on the active field, otherwise switches
   *  to the field with its default direction. */
  toggleSort: (field: Sort) => void;
  /** Display panel select: switches field (default direction), no toggle. */
  setSortField: (field: Sort) => void;
  setSortDirection: (direction: CollectionSortDirection) => void;
  toggleColumn: (key: Column) => void;
  toggleFilter: (key: keyof Filters, value: string) => void;
  clearFilters: () => void;
}

type StoreSet<S> = StoreApi<S>["setState"];
type StoreGet<S> = StoreApi<S>["getState"];

export interface CollectionViewStoreOptions<
  Sort extends string,
  Column extends string,
  Filters extends CollectionFilters<Filters>,
  ExtraState extends object,
  ExtraActions extends object,
> {
  /** Persist key. Changing it orphans every user's saved preferences. */
  name: string;
  version?: number;
  migrate?: (persisted: unknown, version: number) => unknown;
  defaultSortField: Sort;
  sortDefaultDirection: Record<Sort, CollectionSortDirection>;
  defaultHiddenColumns: Column[];
  emptyFilters: Filters;
  /** Page-specific persisted state (scope, view mode). */
  extraState?: ExtraState;
  extraActions?: (
    set: StoreSet<
      CollectionViewCore<Sort, Column, Filters> & ExtraState & ExtraActions
    >,
    get: StoreGet<
      CollectionViewCore<Sort, Column, Filters> & ExtraState & ExtraActions
    >,
  ) => ExtraActions;
  /** Extra state patch applied alongside every filter toggle. */
  onToggleFilter?: (
    state: CollectionViewCore<Sort, Column, Filters> & ExtraState,
  ) => Partial<ExtraState>;
}

export function createCollectionViewStore<
  Sort extends string,
  Column extends string,
  Filters extends CollectionFilters<Filters>,
  ExtraState extends object = Record<never, never>,
  ExtraActions extends object = Record<never, never>,
>(
  options: CollectionViewStoreOptions<
    Sort,
    Column,
    Filters,
    ExtraState,
    ExtraActions
  >,
) {
  type State = CollectionViewCore<Sort, Column, Filters> &
    ExtraState &
    ExtraActions;
  type Persisted = Pick<
    State,
    "sortField" | "sortDirection" | "hiddenColumns" | "filters"
  > &
    ExtraState;

  const extraState = (options.extraState ?? {}) as ExtraState;
  const defaults = {
    sortField: options.defaultSortField,
    sortDirection: options.sortDefaultDirection[options.defaultSortField],
    hiddenColumns: options.defaultHiddenColumns,
    filters: options.emptyFilters,
    ...extraState,
  };
  const extraKeys = Object.keys(extraState) as (keyof ExtraState)[];

  const store = create<State>()(
    persist(
      (set, get) =>
        ({
          ...defaults,
          toggleSort: (field: Sort) =>
            set((state) =>
              state.sortField === field
                ? ({
                    sortDirection:
                      state.sortDirection === "asc" ? "desc" : "asc",
                  } as Partial<State>)
                : ({
                    sortField: field,
                    sortDirection: options.sortDefaultDirection[field],
                  } as Partial<State>),
            ),
          setSortField: (field: Sort) =>
            set((state) =>
              state.sortField === field
                ? {}
                : ({
                    sortField: field,
                    sortDirection: options.sortDefaultDirection[field],
                  } as Partial<State>),
            ),
          setSortDirection: (direction: CollectionSortDirection) =>
            set({ sortDirection: direction } as Partial<State>),
          toggleColumn: (key: Column) =>
            set(
              (state) =>
                ({
                  hiddenColumns: state.hiddenColumns.includes(key)
                    ? state.hiddenColumns.filter((k) => k !== key)
                    : [...state.hiddenColumns, key],
                }) as Partial<State>,
            ),
          toggleFilter: (key: keyof Filters, value: string) =>
            set((state) => {
              const list = state.filters[key] as string[];
              const next = list.includes(value)
                ? list.filter((v) => v !== value)
                : [...list, value];
              return {
                ...options.onToggleFilter?.(state),
                filters: { ...state.filters, [key]: next },
              } as Partial<State>;
            }),
          clearFilters: () =>
            set({ filters: options.emptyFilters } as Partial<State>),
          ...options.extraActions?.(set, get),
        }) as State,
      {
        name: options.name,
        ...(options.version !== undefined ? { version: options.version } : {}),
        ...(options.migrate
          ? {
              migrate: (persisted: unknown, version: number) =>
                options.migrate!(persisted, version) as State,
            }
          : {}),
        storage: createJSONStorage(() =>
          createWorkspaceAwareStorage(defaultStorage),
        ),
        partialize: (state): Persisted => {
          const out = {
            sortField: state.sortField,
            sortDirection: state.sortDirection,
            hiddenColumns: state.hiddenColumns,
            filters: state.filters,
          } as Persisted;
          for (const key of extraKeys) {
            (out as ExtraState)[key] = (state as ExtraState)[key];
          }
          return out;
        },
        // A workspace with nothing persisted resets to the defaults instead
        // of keeping the previous workspace's in-memory state. Filters are
        // deep-merged so a payload saved before a filter dimension existed
        // still gets that key's empty default.
        merge: (persisted, current) => {
          if (!persisted) return { ...current, ...defaults };
          const p = persisted as Partial<State>;
          return {
            ...current,
            ...p,
            filters: { ...options.emptyFilters, ...(p.filters ?? {}) },
          };
        },
      },
    ),
  );

  registerForWorkspaceRehydration(() => store.persist.rehydrate());
  return store;
}
