/**
 * Mobile-local mirror of packages/core/runtimes/hooks.ts's isNewer /
 * runtimeNeedsUpdate. Those are module-private (not exported from
 * @multica/core/runtimes) — the exported hooks built on them
 * (useMyRuntimesNeedUpdate, useUpdatableRuntimeIds) internally call
 * packages/core's OWN runtimeListOptions/latestCliVersionOptions, binding
 * to a different QueryClient/key-factory instance than mobile owns. Same
 * hazard apps/mobile/CLAUDE.md's "Mobile-owned updaters" section documents
 * for realtime WS updaters — mirror the comparison logic instead of trying
 * to reuse the hooks. readRuntimeCliVersion IS imported below since it's
 * actually exported and purely reads a field off `metadata`.
 */
import type { RuntimeDevice } from "@multica/core/types";
import { readRuntimeCliVersion } from "@multica/core/runtimes";

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

/**
 * Whether to show a static "update available" badge for this runtime.
 * Mirrors desktop's exact gating (packages/core/runtimes/hooks.ts's
 * runtimeNeedsUpdate): local runtimes only, only for the signed-in owner,
 * never for desktop-launched runtimes (Desktop has its own auto-updater).
 */
export function runtimeNeedsUpdate(
  runtime: RuntimeDevice,
  latestVersion: string | null | undefined,
  userId: string | null | undefined,
): boolean {
  if (!latestVersion || !userId) return false;
  if (runtime.runtime_mode !== "local") return false;
  if (runtime.owner_id !== userId) return false;
  if (runtime.metadata && runtime.metadata.launched_by === "desktop") {
    return false;
  }
  const cliVersion = readRuntimeCliVersion(runtime.metadata);
  if (!cliVersion) return false;
  return isNewer(latestVersion, cliVersion);
}
