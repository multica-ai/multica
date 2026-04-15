import type { StorageAdapter } from "../types/storage";

/**
 * Keys that are namespaced per workspace (stored as `${key}:${wsId}`).
 *
 * IMPORTANT: When adding a new workspace-scoped persist store or storage key,
 * add its key here so that workspace deletion and logout properly clean it up.
 * Also ensure the store uses `createWorkspaceAwareStorage` for its persist config.
 */
const WORKSPACE_SCOPED_KEYS = [
  "multica_issue_draft",
  "multica_issues_view",
  "multica_issues_scope",
  "multica_my_issues_view",
  "multica:chat:selectedAgentId",
  "multica:chat:activeSessionId",
  "multica:chat:drafts",
  "multica:chat:expanded",
];

/** Remove all workspace-scoped storage entries for the given workspace. */
export function clearWorkspaceStorage(
  adapter: StorageAdapter,
  wsId: string,
) {
  for (const key of WORKSPACE_SCOPED_KEYS) {
    adapter.removeItem(`${key}:${wsId}`);
  }
}
