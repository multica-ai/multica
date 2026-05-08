import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import type { AgentRuntime } from "../types";
import { isCliVersionNewer } from "./cli-version";
import { runtimeListOptions, latestCliVersionOptions } from "./queries";

function runtimeNeedsUpdate(
  rt: AgentRuntime,
  latestVersion: string,
  userId: string,
): boolean {
  if (rt.runtime_mode !== "local") return false;
  // Only show to the user who owns this runtime.
  if (rt.owner_id !== userId) return false;
  // Desktop-managed runtimes are updated by the Desktop app's own auto-updater;
  // the platform should not surface CLI update prompts for them.
  if (rt.metadata && rt.metadata.launched_by === "desktop") {
    return false;
  }
  const cliVersion =
    rt.metadata && typeof rt.metadata.cli_version === "string"
      ? rt.metadata.cli_version
      : null;
  if (!cliVersion) return false;
  return isCliVersionNewer(latestVersion, cliVersion);
}

export function useRuntimeList(wsId: string, owner?: "me") {
  return useQuery(runtimeListOptions(wsId, owner));
}

/**
 * Returns true if the current user has any local runtime with an outdated CLI version.
 * Accepts wsId as parameter so callers outside WorkspaceIdProvider can use it safely.
 */
export function useMyRuntimesNeedUpdate(wsId: string | undefined): boolean {
  const userId = useAuthStore((s) => s.user?.id);
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: latestVersion } = useQuery(latestCliVersionOptions());

  if (!runtimes || !latestVersion || !userId) return false;

  return runtimes.some((rt) => runtimeNeedsUpdate(rt, latestVersion, userId));
}

/**
 * Returns a Set of runtime IDs that belong to the current user and have updates available.
 * Accepts wsId as parameter so callers outside WorkspaceIdProvider can use it safely.
 */
export function useUpdatableRuntimeIds(wsId: string | undefined): Set<string> {
  const userId = useAuthStore((s) => s.user?.id);
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: latestVersion } = useQuery(latestCliVersionOptions());

  return useMemo(() => {
    if (!runtimes || !latestVersion || !userId) return new Set<string>();
    const ids = new Set<string>();
    for (const rt of runtimes) {
      if (runtimeNeedsUpdate(rt, latestVersion, userId)) {
        ids.add(rt.id);
      }
    }
    return ids;
  }, [runtimes, latestVersion, userId]);
}
