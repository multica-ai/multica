"use client";

import { useCurrentWorkspace } from "./paths/hooks";

/**
 * Returns the current workspace UUID. Throws if called outside a workspace route.
 *
 * Implementation: derives from useCurrentWorkspace() (URL slug + React Query list).
 * No longer backed by a React Context — the WorkspaceIdProvider has been removed
 * as part of the slug-first refactor. The throw semantics are preserved so existing
 * callers that depend on non-null don't need guard code.
 */
export function useWorkspaceId(): string {
  const ws = useCurrentWorkspace();
  if (!ws) throw new Error("useWorkspaceId: no workspace selected — ensure component renders inside a workspace route");
  return ws.id;
}
