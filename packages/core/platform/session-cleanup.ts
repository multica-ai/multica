import type { QueryClient } from "@tanstack/react-query";
import { resetAllRegisteredDrafts } from "../drafts/cleanup-registry";
import type { StorageAdapter } from "../types/storage";
import type { Workspace } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { defaultStorage } from "./storage";
import {
  clearAllWorkspaceStorage,
  clearWorkspaceStorage,
} from "./storage-cleanup";

/**
 * Erase everything the finished session left on this client: in-memory draft
 * stores, per-workspace persisted state, the desktop tab layout, the
 * last-workspace cookie, and the React Query cache.
 *
 * Both ways a session can end share this. An explicit logout is the obvious
 * one; a session the server ended (401) needs it just as much, because that
 * screen offers a login form and the next person to use it may not be the one
 * who was just signed in. Skipping it there would hand a colleague on a shared
 * machine the previous user's drafts, chat selection, and tab titles — the
 * exact leak `drafts/cleanup-registry` was written to close.
 *
 * What it deliberately does not touch: auth state itself (callers own that,
 * and the two paths publish it differently) and anything belonging to a
 * process that outlives the session, such as Desktop's local daemon.
 */
export function clearClientSessionData(
  queryClient: QueryClient,
  storage: StorageAdapter = defaultStorage,
): void {
  // Reset draft stores' in-memory state FIRST, before removing persisted
  // keys. Each reset is a Zustand setState, and persist middleware writes the
  // new (empty) state straight back to storage under the still-active
  // workspace slug — so resetting after removal would resurrect the very keys
  // just deleted, with whatever the reset state still carries. Memory must be
  // wiped regardless of the workspace list below: ending a session does not
  // reload the page, so the singletons would otherwise surface the previous
  // user's draft after the next login.
  resetAllRegisteredDrafts();

  // Then clear workspace-scoped storage, BEFORE clearing the React Query cache
  // (which holds the workspace list). Otherwise per-workspace drafts/chat/etc
  // would leak to the next user on this device.
  //
  // Enumerating the stored keys rather than the workspace list is what makes
  // this work on a cold start: a session rejected at the identity probe never
  // loaded a workspace list, so there would be no slugs to iterate and every
  // per-workspace key would survive. Adapters that cannot list keys get the
  // narrower sweep over whatever workspaces this process did resolve.
  if (!clearAllWorkspaceStorage(storage)) {
    const cachedWorkspaces =
      queryClient.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    for (const ws of cachedWorkspaces) {
      clearWorkspaceStorage(storage, ws.slug);
    }
  }

  // Clear the last-workspace-slug cookie. Otherwise on a shared device the
  // next user gets redirected by the proxy to the previous user's last
  // workspace, then bounced to NoAccessPage — confusing.
  if (typeof document !== "undefined") {
    document.cookie = "last_workspace_slug=; path=/; max-age=0; SameSite=Lax";
  }

  // Clear desktop tab state. Tab paths can contain workspace slugs and issue
  // UUIDs that must not survive across user sessions on a shared machine.
  // No-op on web (web doesn't write this key).
  storage.removeItem("multica_tabs");

  queryClient.clear();
}
