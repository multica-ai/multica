import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import type { AgentRuntime } from "../types";
import { runtimeListOptions, latestCliVersionOptions } from "./queries";

function stripV(v: string): string {
  return v.replace(/^v/, "");
}

function isNewer(latest: string, current: string): boolean {
  const l = stripV(latest).split(".").map(Number);
  const c = stripV(current).split(".").map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0;
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

function runtimeNeedsUpdate(
  rt: AgentRuntime,
  latestVersion: string,
  userId: string,
): boolean {
  if (rt.runtime_mode !== "local") return false;
  // Only show to the user who owns this runtime.
  if (rt.owner_id !== userId) return false;
  const cliVersion =
    rt.metadata && typeof rt.metadata.cli_version === "string"
      ? rt.metadata.cli_version
      : null;
  if (!cliVersion) return false;
  return isNewer(latestVersion, cliVersion);
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
