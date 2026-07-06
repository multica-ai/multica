"use client";

import { useCurrentWorkspace } from "./paths/hooks";
import { getCurrentWsId } from "./platform/workspace-storage";

/**
 * Returns the current workspace UUID. Throws if called outside a workspace route.
 *
 * Implementation: derives from useCurrentWorkspace() (URL slug + React Query list).
 * During app boot, the workspace route layout also mirrors the resolved id into
 * workspace-storage before rendering children; use that mirror as a narrow
 * fallback so route children don't crash during React Query/cache handoff.
 */
export function useWorkspaceId(): string {
  const ws = useCurrentWorkspace();
  if (ws) return ws.id;
  const currentWsId = getCurrentWsId();
  if (currentWsId) return currentWsId;
  throw new Error("useWorkspaceId: no workspace selected — ensure component renders inside a workspace route");
}
