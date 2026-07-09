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
 * Step 3's runtime data layer, shared by Desktop (`StepRuntimeConnect`)
 * and Web (`StepPlatformFork`):
 *
 *   - Polls every 2s while the list is empty so the UI flips to
 *     "found" the moment a runtime registers.
 *   - `daemon:register` WS event triggers an instant refetch — no
 *     polling lag for online users.
 *   - Auto-selects online first, falls back to the first runtime.
 *     Only runs when the user hasn't picked anything, so a manual
 *     selection survives subsequent refetches.
 */
export function useRuntimePicker(wsId: string): {
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  hasRuntimes: boolean;
} {
  const qc = useQueryClient();

  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId, "me"),
    refetchInterval: (q) => (q.state.data?.length ? false : 2000),
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
