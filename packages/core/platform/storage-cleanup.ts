import type { StorageAdapter } from "../types/storage";
import {
  clearRegisteredWorkspaceDrafts,
  registeredDraftKeys,
} from "../drafts/cleanup-registry";
// Ensure every module-level draft store has registered its key before cleanup
// runs, so the registry is never partially populated at logout/delete time.
import "../drafts/register-all-drafts";

/**
 * Non-draft workspace-scoped keys (stored as `${key}:${slug}`).
 *
 * Draft stores no longer live here: they self-register via
 * `registerDraftCleanup` (see `drafts/cleanup-registry`) and are cleared by
 * `clearRegisteredWorkspaceDrafts` below. This list is only for the remaining
 * view/navigation keys that are not drafts.
 *
 * IMPORTANT: When adding a new non-draft workspace-scoped persist store, add
 * its key here; for draft stores, prefer `createDraftStore` (auto-registers)
 * or call `registerDraftCleanup` directly.
 */
const WORKSPACE_SCOPED_KEYS = [
  "multica_issue_surface_views",
  "multica_issues_view",
  "multica_issues_scope",
  "multica_my_issues_view",
  "multica:chat:selectedAgentId",
  "multica:chat:selectedProjectId",
  "multica:chat:activeSessionId",
  "multica:chat:expanded",
  "multica_navigation",
];

/** Remove all workspace-scoped storage entries for the given workspace slug. */
export function clearWorkspaceStorage(
  adapter: StorageAdapter,
  slug: string,
) {
  for (const key of WORKSPACE_SCOPED_KEYS) {
    adapter.removeItem(`${key}:${slug}`);
  }
  // Draft stores self-register their keys; clear them from the registry so a
  // new draft store can never be silently skipped by an out-of-date list.
  clearRegisteredWorkspaceDrafts(adapter, slug);
}

/**
 * Remove workspace-scoped storage for EVERY workspace on this device, without
 * being told which ones exist.
 *
 * `clearWorkspaceStorage` needs a slug, and the slugs come from the workspace
 * list — which a session rejected at the identity probe never loaded. A cold
 * start with a stale token therefore had no way to clean up after the previous
 * session, leaving `multica_comment_drafts:<slug>` and friends for whoever
 * signed in next. Enumerating the keys removes that dependency entirely, and
 * also catches workspaces the last session had left before it ended.
 *
 * Returns false when the adapter cannot list its keys, so the caller can say
 * what it is falling back to rather than silently doing less.
 */
export function clearAllWorkspaceStorage(adapter: StorageAdapter): boolean {
  const storedKeys = adapter.keys?.();
  if (!storedKeys) return false;

  const drafts = registeredDraftKeys();
  const prefixes = [...WORKSPACE_SCOPED_KEYS, ...drafts.workspaceScoped].map(
    (base) => `${base}:`,
  );
  for (const key of storedKeys) {
    if (prefixes.some((prefix) => key.startsWith(prefix))) {
      adapter.removeItem(key);
    }
  }
  // Globally-namespaced draft keys carry no slug to match on.
  for (const key of drafts.global) adapter.removeItem(key);
  return true;
}
