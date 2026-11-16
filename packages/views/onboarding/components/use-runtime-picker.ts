"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWSEvent } from "@multica/core/realtime";
import {
  runtimeKeys,
  runtimeListOptions,
} from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";

/**
 * Empty-list fallback interval. Reads only `query.state`: a non-empty
 * list stops the poll, and `dataUpdateCount + errorUpdateCount` climbs
 * 2s → 5s → 15s. Both counters are monotonic, so re-evaluating this on
 * every render stays on the same rung, and a list that goes empty again
 * stays at the 15s cap. `daemon:register` is the instant path.
 */
export function runtimePickerRefetchInterval(query: {
  state: {
    data?: { length: number } | undefined;
    dataUpdateCount: number;
    errorUpdateCount: number;
  };
}): number | false {
  if ((query.state.data?.length ?? 0) > 0) return false;
  const completed = query.state.dataUpdateCount + query.state.errorUpdateCount;
  if (completed <= 1) return 2000;
  if (completed === 2) return 5000;
  return 15000;
}

/**
 * Step 3's runtime data layer, shared by Desktop (`StepRuntimeConnect`)
 * and Web (`StepPlatformFork`):
 *
 *   - While the list is empty, polls at 2s, then 5s, then stays at 15s
 *     so the UI flips to "found" when a runtime registers. An install
 *     can take minutes, so the slow fallback keeps going.
 *   - `daemon:register` WS event triggers an instant refetch — no
 *     polling lag for online users.
 *   - Auto-selects online first, falls back to the first runtime.
 *     Only runs when the user hasn't picked anything, so a manual
 *     selection survives subsequent refetches.
 */
export function useRuntimePicker(wsId: string, wsSlug?: string): {
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  hasRuntimes: boolean;
} {
  const qc = useQueryClient();

  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId, "me", wsSlug),
    refetchInterval: runtimePickerRefetchInterval,
  });

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (selectedId) return;
    const preferred =
      runtimes.find((r) => r.status === "online") ?? runtimes[0];
    if (preferred) setSelectedId(preferred.id);
  }, [runtimes, selectedId]);

  const selected = runtimes.find((r) => r.id === selectedId) ?? null;

  return {
    runtimes,
    selected,
    selectedId,
    setSelectedId,
    hasRuntimes: runtimes.length > 0,
  };
}
