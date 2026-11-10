"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWSEvent } from "@multica/core/realtime";
import {
  runtimeKeys,
  runtimeListOptions,
} from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import {
  isDesktopShell,
  restartLocalDaemon,
  useLocalDaemonStatus,
} from "../../platform";

/** Ceiling on "still detecting": a version probe that never finishes must
 *  not keep the picker on its loading rows forever. */
const DETECT_TIMEOUT_MS = 20_000;

/** Poll interval while onboarding waits for a tool to appear. A tool installed
 *  from a terminal mid-step registers without any event this page can hear,
 *  so the list keeps polling for as long as it is on screen. */
const POLL_MS = 3_000;

/**
 * The member's own runtimes in the workspace being set up, plus whether more
 * are still on their way.
 *
 * "Still on their way" is the desktop daemon's word for it: it is booting, or
 * it found a CLI on PATH that has not registered yet. Showing the missing-tool
 * install rows during that window would tell someone with Claude Code
 * installed to go install Claude Code.
 */
export function useOnboardingRuntimes(wsId: string, wsSlug?: string) {
  const qc = useQueryClient();
  const daemon = useLocalDaemonStatus();

  const { data, isPending } = useQuery({
    ...runtimeListOptions(wsId, "me", wsSlug),
    refetchInterval: POLL_MS,
  });

  const invalidate = useCallback(
    () => qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) }),
    [qc, wsId],
  );
  useWSEvent("daemon:register", invalidate);

  const runtimes = useMemo(() => sortRuntimes(data ?? []), [data]);

  const [epoch, setEpoch] = useState(0);
  const [timedOut, setTimedOut] = useState(false);
  useEffect(() => {
    setTimedOut(false);
    const timer = window.setTimeout(() => setTimedOut(true), DETECT_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [epoch]);

  const registered = new Set(runtimes.map((r) => r.provider));
  const unregistered = daemon.agents.filter((a) => !registered.has(a));
  const detecting =
    !timedOut &&
    (isPending || daemon.starting || (daemon.running && unregistered.length > 0));

  const canRescan = isDesktopShell();
  const rescan = useCallback(async () => {
    await restartLocalDaemon();
    setEpoch((n) => n + 1);
    await invalidate();
  }, [invalidate]);

  return {
    runtimes,
    /** Tools are still being detected; hold off on "not installed". */
    detecting,
    /** Restarting the bundled daemon makes it scan PATH again (desktop). */
    canRescan,
    rescan,
  };
}

function sortRuntimes(runtimes: AgentRuntime[]): AgentRuntime[] {
  return [...runtimes].sort((a, b) => {
    if (a.status !== b.status) return a.status === "online" ? -1 : 1;
    return a.created_at.localeCompare(b.created_at);
  });
}
