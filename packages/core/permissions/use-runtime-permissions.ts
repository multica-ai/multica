"use client";

import type { AgentRuntime } from "../types";
import { useCurrentMember } from "./use-current-member";
import {
  canControlNodeRun,
  canManageRuntimePermissions,
} from "./rules";
import { deny, type Decision } from "./types";

const PENDING: Decision = deny("unknown", "");

/**
 * Resolves the current user's capabilities on a runtime.
 * `wsId` is explicit so the hook works outside `WorkspaceIdProvider`.
 */
export function useRuntimePermissions(
  runtime: AgentRuntime | null,
  wsId: string,
): {
  canManagePermissions: Decision;
} {
  const { userId, role } = useCurrentMember(wsId);
  const ctx = { userId, role };
  if (runtime === null) {
    return { canManagePermissions: PENDING };
  }
  return {
    canManagePermissions: canManageRuntimePermissions(runtime, ctx),
  };
}

/**
 * Resolves whether the current user may control a node-run whose runtime
 * reports `canControl`. The backend is the source of truth; this hook just
 * adapts the capability into the shared `Decision` shape.
 */
export function useNodeRunControlPermission(
  canControl: boolean,
  wsId: string,
): Decision {
  const { userId, role } = useCurrentMember(wsId);
  return canControlNodeRun(canControl, { userId, role });
}
