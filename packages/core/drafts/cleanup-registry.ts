import type { StorageAdapter } from "../types/storage";
import { abortAll as abortAllUploads } from "./upload-coordinator";

/**
 * Self-registration registry for draft stores, replacing the hand-maintained
 * `WORKSPACE_SCOPED_KEYS` array in `platform/storage-cleanup.ts`.
 *
 * The old model required every new draft store to remember to append its
 * persist key to a list living in a different file; that list drifted and
 * left `multica_comment_drafts`, `multica_quick_create`, `multica_project_draft`,
 * `multica_feedback_draft`, and the chat draft-attachment / restore keys
 * uncleared on logout and workspace deletion (persistence-layer leak), while
 * the in-memory Zustand singletons kept a previous user's draft after a
 * client-side logout navigation (memory-layer leak, cross-user on a shared
 * profile).
 *
 * A store registers ONCE at module load via `registerDraftCleanup`. Cleanup
 * then iterates the registry, so adding a draft store can never again silently
 * skip cleanup.
 */
export interface DraftCleanupEntry {
  /**
   * Base persist key, before any workspace-slug suffix. For workspace-scoped
   * stores the real localStorage key is `${storageKey}:${slug}`.
   */
  storageKey: string;
  /**
   * True when persisted through `createWorkspaceAwareStorage` (key suffixed
   * with the active slug). False for globally-namespaced keys.
   */
  workspaceScoped: boolean;
  /**
   * Reset this store's in-memory state to empty. Called on logout so the
   * Zustand singleton cannot surface a previous user's draft after a
   * client-side navigation (no full page reload clears module singletons).
   */
  resetInMemory: () => void;
}

const entries = new Map<string, DraftCleanupEntry>();

/** Register a draft store for workspace/logout cleanup. Idempotent per key. */
export function registerDraftCleanup(entry: DraftCleanupEntry): void {
  entries.set(entry.storageKey, entry);
}

/**
 * Remove every registered draft's persisted storage for one workspace slug.
 * Called on workspace delete/leave (per slug) and logout (per workspace the
 * user belonged to). Globally-namespaced draft keys are removed regardless of
 * slug — passing any slug clears them once.
 */
export function clearRegisteredWorkspaceDrafts(
  adapter: StorageAdapter,
  slug: string,
): void {
  for (const entry of entries.values()) {
    if (entry.workspaceScoped) {
      adapter.removeItem(`${entry.storageKey}:${slug}`);
    } else {
      adapter.removeItem(entry.storageKey);
    }
  }
}

/**
 * Reset all registered draft stores' in-memory state. Called on logout before
 * auth is cleared, so no draft survives into the next login on the same tab.
 */
export function resetAllRegisteredDrafts(): void {
  // Abort in-flight uploads BEFORE clearing drafts: an upload that settles
  // after the draft is wiped must not resurrect a placeholder, and its bytes
  // must never bind an attachment under the next session (MUL-5181).
  abortAllUploads();
  for (const entry of entries.values()) {
    entry.resetInMemory();
  }
}

/** Test-only: drop all registrations. */
export function __clearDraftCleanupRegistryForTest(): void {
  entries.clear();
}

/** Test-only: inspect registered keys. */
export function __getRegisteredDraftKeysForTest(): string[] {
  return [...entries.keys()];
}
