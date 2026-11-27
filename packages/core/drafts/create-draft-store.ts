import { create, type StoreApi, type UseBoundStore } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";
import { registerDraftCleanup } from "./cleanup-registry";

/**
 * Factory for singleton draft stores. Before this existed, every simple draft
 * store (`projects/draft-store`, `feedback/draft-store`, and structurally the
 * issue draft store) hand-copied the identical shape: a `draft` object,
 * `setDraft(patch)`, `clearDraft()`, `hasDraft()`, a `persist` config wired to
 * `createWorkspaceAwareStorage`, and a `registerForWorkspaceRehydration` call.
 * The only per-store differences are the data shape, the persist key, and the
 * "is this draft meaningful" predicate — exactly the {@link DraftStoreConfig}
 * fields below.
 *
 * The factory additionally wires two things the copies kept getting wrong:
 *   1. `registerDraftCleanup` — self-registers the persist key so workspace
 *      delete and logout clean it up without a hand-maintained key list.
 *   2. an in-memory `resetInMemory` — logout resets the Zustand singleton so a
 *      previous user's draft cannot survive into the next login on the same tab.
 */
export interface DraftStoreConfig<TData> {
  /** Persist key, before the workspace-slug suffix. Must be globally unique. */
  storageKey: string;
  /** The empty/initial draft. Cleared drafts and fresh stores start here. */
  emptyData: TData;
  /**
   * Whether the draft holds recoverable user intent. Must consider every
   * signal the surface treats as "unsaved work" — text, meaningful field
   * selections, attachments — not just the primary text field.
   */
  hasMeaningful: (data: TData) => boolean;
  /** Persist per workspace slug via `createWorkspaceAwareStorage`. Default true. */
  workspaceScoped?: boolean;
  /**
   * Backfill defaults onto drafts persisted by older builds that predate a
   * later-added field, so read sites can rely on the declared shape. Receives
   * the raw persisted `data` and returns the fields to overlay onto emptyData.
   */
  migrateData?: (persistedData: unknown) => Partial<TData>;
}

export interface DraftStore<TData> {
  draft: TData;
  setDraft: (patch: Partial<TData>) => void;
  clearDraft: () => void;
  hasDraft: () => boolean;
}

export function createDraftStore<TData extends object>(
  config: DraftStoreConfig<TData>,
): UseBoundStore<StoreApi<DraftStore<TData>>> {
  const workspaceScoped = config.workspaceScoped ?? true;
  const empty = () => structuredCloneData(config.emptyData);

  const useStore = create<DraftStore<TData>>()(
    persist(
      (set, get) => ({
        draft: empty(),
        setDraft: (patch) => set((s) => ({ draft: { ...s.draft, ...patch } })),
        clearDraft: () => set({ draft: empty() }),
        hasDraft: () => config.hasMeaningful(get().draft),
      }),
      {
        name: config.storageKey,
        storage: createJSONStorage(() =>
          workspaceScoped
            ? createWorkspaceAwareStorage(defaultStorage)
            : defaultStorage,
        ),
        merge: (persistedState, currentState) => {
          const persisted = (persistedState ?? {}) as Partial<DraftStore<TData>>;
          const persistedData = persisted.draft as unknown;
          const overlay = config.migrateData
            ? config.migrateData(persistedData)
            : (persistedData as Partial<TData> | undefined);
          return {
            ...currentState,
            ...persisted,
            // Clone emptyData so a persisted draft missing a nested field does
            // not alias the module-level constant's array/object references.
            draft: { ...structuredCloneData(config.emptyData), ...overlay },
          };
        },
      },
    ),
  );

  if (workspaceScoped) {
    registerForWorkspaceRehydration(() => useStore.persist.rehydrate());
  }

  registerDraftCleanup({
    storageKey: config.storageKey,
    workspaceScoped,
    resetInMemory: () => useStore.getState().clearDraft(),
  });

  return useStore;
}

// Structured clone kept local and defensive: the empty draft may contain
// nested arrays/objects (labelIds, propertyValues, attachments) that must not
// be shared by reference between the initial state and later `clearDraft`.
function structuredCloneData<T>(value: T): T {
  return typeof structuredClone === "function"
    ? structuredClone(value)
    : (JSON.parse(JSON.stringify(value)) as T);
}
