import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { RuntimePingStatus, RuntimeUpdateStatus, StorageAdapter } from "../types";
import {
  createPersistStorage,
  createWorkspaceAwareStorage,
  defaultStorage,
  registerForWorkspaceRehydration,
} from "../platform";

export const RUNTIME_PING_STORAGE_KEY = "multica_runtime_ping";
export const DAEMON_UPDATE_STORAGE_KEY = "multica_daemon_update";

export const PING_RECOVERY_WINDOW_MS = 90 * 1000;
export const PING_TERMINAL_TTL_MS = 10 * 60 * 1000;
export const UPDATE_RECOVERY_WINDOW_MS = 180 * 1000;
export const UPDATE_COMPLETED_TTL_MS = 30 * 60 * 1000;

export interface RuntimePingEntry {
  runtimeId: string;
  requestId: string;
  status: RuntimePingStatus;
  startedAt: number;
  finishedAt?: number;
  output?: string;
  error?: string;
  durationMs?: number | null;
}

export interface DaemonUpdateEntry {
  daemonId: string;
  runtimeId: string;
  requestId: string;
  targetVersion: string;
  status: RuntimeUpdateStatus;
  startedAt: number;
  finishedAt?: number;
  output?: string;
  error?: string;
}

interface RuntimePingStoreState {
  entries: Record<string, RuntimePingEntry>;
  setEntry: (entry: RuntimePingEntry) => void;
  updateEntry: (runtimeId: string, patch: Partial<RuntimePingEntry>) => void;
  clearEntry: (runtimeId: string) => void;
  cleanupExpired: (now?: number) => void;
}

interface DaemonUpdateStoreState {
  entries: Record<string, DaemonUpdateEntry>;
  setEntry: (entry: DaemonUpdateEntry) => void;
  updateEntry: (daemonId: string, patch: Partial<DaemonUpdateEntry>) => void;
  clearEntry: (daemonId: string) => void;
  cleanupExpired: (now?: number) => void;
}

function isPingTerminal(status: RuntimePingStatus): boolean {
  return (
    status === "completed" ||
    status === "failed" ||
    status === "timeout" ||
    status === "interrupted"
  );
}

function normalizePingEntry(
  entry: RuntimePingEntry,
  now: number,
): RuntimePingEntry | null {
  if (
    (entry.status === "pending" || entry.status === "running") &&
    now - entry.startedAt > PING_RECOVERY_WINDOW_MS
  ) {
    return {
      ...entry,
      status: "timeout",
      error: entry.error ?? "Test status recovery timed out",
      finishedAt: entry.finishedAt ?? now,
    };
  }

  const terminalAt = entry.finishedAt ?? entry.startedAt;
  if (isPingTerminal(entry.status) && now - terminalAt > PING_TERMINAL_TTL_MS) {
    return null;
  }

  return entry;
}

function normalizeUpdateEntry(
  entry: DaemonUpdateEntry,
  now: number,
): DaemonUpdateEntry | null {
  if (
    (entry.status === "pending" ||
      entry.status === "running" ||
      entry.status === "interrupted") &&
    now - entry.startedAt > UPDATE_RECOVERY_WINDOW_MS
  ) {
    return {
      ...entry,
      status: "timeout",
      error: entry.error ?? "Update status recovery timed out",
      finishedAt: entry.finishedAt ?? now,
    };
  }

  if (entry.status === "completed") {
    const terminalAt = entry.finishedAt ?? entry.startedAt;
    if (now - terminalAt > UPDATE_COMPLETED_TTL_MS) {
      return null;
    }
  }

  return entry;
}

function rehydrateAndCleanup<T extends { cleanupExpired: () => void }>(store: {
  persist: { rehydrate: () => void | Promise<void> };
  getState: () => T;
}) {
  return () => {
    void Promise.resolve(store.persist.rehydrate()).then(() => {
      store.getState().cleanupExpired();
    });
  };
}

export function createRuntimePingStore(storageAdapter: StorageAdapter) {
  return create<RuntimePingStoreState>()(
    persist(
      (set) => ({
        entries: {},
        setEntry: (entry) =>
          set((state) => ({
            entries: {
              ...state.entries,
              [entry.runtimeId]: entry,
            },
          })),
        updateEntry: (runtimeId, patch) =>
          set((state) => {
            const existing = state.entries[runtimeId];
            if (!existing) return state;

            return {
              entries: {
                ...state.entries,
                [runtimeId]: {
                  ...existing,
                  ...patch,
                },
              },
            };
          }),
        clearEntry: (runtimeId) =>
          set((state) => {
            const entries = { ...state.entries };
            delete entries[runtimeId];
            return { entries };
          }),
        cleanupExpired: (now = Date.now()) =>
          set((state) => {
            const entries: Record<string, RuntimePingEntry> = {};

            for (const [runtimeId, entry] of Object.entries(state.entries)) {
              const normalized = normalizePingEntry(entry, now);
              if (normalized) {
                entries[runtimeId] = normalized;
              }
            }

            return { entries };
          }),
      }),
      {
        name: RUNTIME_PING_STORAGE_KEY,
        storage: createJSONStorage(() =>
          createWorkspaceAwareStorage(storageAdapter),
        ),
        partialize: (state) => ({ entries: state.entries }),
        merge: (persistedState, currentState) => ({
          ...currentState,
          entries:
            (persistedState as Partial<RuntimePingStoreState> | undefined)?.entries ??
            {},
        }),
      },
    ),
  );
}

export function createDaemonUpdateStore(storageAdapter: StorageAdapter) {
  return create<DaemonUpdateStoreState>()(
    persist(
      (set) => ({
        entries: {},
        setEntry: (entry) =>
          set((state) => ({
            entries: {
              ...state.entries,
              [entry.daemonId]: entry,
            },
          })),
        updateEntry: (daemonId, patch) =>
          set((state) => {
            const existing = state.entries[daemonId];
            if (!existing) return state;

            return {
              entries: {
                ...state.entries,
                [daemonId]: {
                  ...existing,
                  ...patch,
                },
              },
            };
          }),
        clearEntry: (daemonId) =>
          set((state) => {
            const entries = { ...state.entries };
            delete entries[daemonId];
            return { entries };
          }),
        cleanupExpired: (now = Date.now()) =>
          set((state) => {
            const entries: Record<string, DaemonUpdateEntry> = {};

            for (const [daemonId, entry] of Object.entries(state.entries)) {
              const normalized = normalizeUpdateEntry(entry, now);
              if (normalized) {
                entries[daemonId] = normalized;
              }
            }

            return { entries };
          }),
      }),
      {
        name: DAEMON_UPDATE_STORAGE_KEY,
        storage: createJSONStorage(() => createPersistStorage(storageAdapter)),
        partialize: (state) => ({ entries: state.entries }),
        merge: (persistedState, currentState) => ({
          ...currentState,
          entries:
            (persistedState as Partial<DaemonUpdateStoreState> | undefined)
              ?.entries ?? {},
        }),
      },
    ),
  );
}

export const useRuntimePingStore = createRuntimePingStore(defaultStorage);
export const useDaemonUpdateStore = createDaemonUpdateStore(defaultStorage);

registerForWorkspaceRehydration(rehydrateAndCleanup(useRuntimePingStore));
void Promise.resolve(useRuntimePingStore.persist.rehydrate()).then(() => {
  useRuntimePingStore.getState().cleanupExpired();
});
void Promise.resolve(useDaemonUpdateStore.persist.rehydrate()).then(() => {
  useDaemonUpdateStore.getState().cleanupExpired();
});

export function resolveUpdateScopeId(daemonId: string | null, runtimeId: string) {
  return daemonId || runtimeId;
}
