"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { runtimeListOptions } from "./queries";
import { deriveRuntimeHealth } from "./derive-health";
import type { RuntimeHealth } from "./types";

// Re-render every 30s so transitions like recently_lost → offline (which
// happens at the 5-minute mark with no new data) reflect in the UI.
const HEALTH_TICK_MS = 30_000;

function useHealthTick(): number {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), HEALTH_TICK_MS);
    return () => clearInterval(id);
  }, []);
  return tick;
}

/**
 * Derived runtime health (online / recently_lost / offline / about_to_gc),
 * or "loading" while the runtime list is still resolving.
 *
 * Accepts wsId as a parameter so the hook works outside WorkspaceIdProvider.
 */
export function useRuntimeHealth(
  wsId: string | undefined,
  runtimeId: string | undefined,
): RuntimeHealth | "loading" {
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const tick = useHealthTick();

  return useMemo<RuntimeHealth | "loading">(() => {
    if (!wsId || !runtimeId) return "loading";
    if (!runtimes) return "loading";
    const runtime = runtimes.find((r) => r.id === runtimeId);
    if (!runtime) return "loading";
    return deriveRuntimeHealth(runtime, Date.now());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId, runtimeId, runtimes, tick]);
}
