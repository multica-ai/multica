import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import type { AgentRuntime, RuntimeModelPricing } from "../types";
import { runtimeListOptions, latestCliVersionOptions } from "./queries";
import { useManifestPricingStore } from "./manifest-pricing-store";

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

/**
 * Mirror runtime-extension manifest pricing into the global manifest
 * pricing store. Mounted near the top of the runtime tree (e.g. inside
 * the workspace shell) so any cost-estimation helper that reads from
 * `getManifestPricing()` sees rates as soon as the runtime list arrives.
 *
 * No-op when no runtimes carry pricing — built-in runtimes always omit
 * the field, so this is effectively a hook for external runtime
 * extensions only.
 */
export function useSyncManifestPricing(wsId: string | undefined): void {
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const setManifestPricings = useManifestPricingStore(
    (s) => s.setManifestPricings,
  );

  useEffect(() => {
    if (!runtimes) return;
    const next: Record<string, RuntimeModelPricing> = {};
    for (const rt of runtimes) {
      const pricing = rt.pricing;
      if (!pricing) continue;
      for (const [modelId, p] of Object.entries(pricing)) {
        // Last-write-wins across runtimes that share a model id. The
        // daemon enforces unique provider keys, so collisions are rare;
        // we accept the simpler merge instead of growing a per-provider
        // namespace.
        next[modelId] = p;
      }
    }
    // Replace, don't merge: the runtime list is authoritative for the
    // currently-installed manifests. Removing a runtime should drop its
    // rates immediately.
    setManifestPricings(next);
  }, [runtimes, setManifestPricings]);
}
