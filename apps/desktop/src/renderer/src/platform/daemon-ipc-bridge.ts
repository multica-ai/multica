"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { runtimeKeys } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";

/**
 * DesktopAPI exposes a richer DaemonStatus shape than the public AgentRuntime
 * type — we redeclare the fields we consume here to avoid coupling the bridge
 * to the desktop preload typings (which live in apps/desktop/src/preload).
 */
interface DaemonStatusLike {
  state: "running" | "stopped" | "starting" | "stopping" | "installing_cli" | "cli_not_found";
  daemonId?: string;
}

/**
 * Merges a local DaemonStatus into an AgentRuntime row. Only the `status`
 * field is overridden; other fields (name, provider, last_seen_at, etc)
 * remain server-authoritative. We deliberately ignore intermediate states
 * (starting / stopping / installing_cli / cli_not_found) so the cache
 * doesn't flap during boot — if the daemon is in such a state, the runtime
 * is effectively offline anyway, and the server-side sweeper will mark it
 * within 75s.
 */
function mergeDaemonStatus(rt: AgentRuntime, status: DaemonStatusLike): AgentRuntime {
  if (status.state === "stopped" || status.state === "stopping") {
    return { ...rt, status: "offline" };
  }
  if (status.state === "running") {
    return {
      ...rt,
      status: "online",
      last_seen_at: new Date().toISOString(),
    };
  }
  return rt;
}

/**
 * Subscribes to local daemon status changes via Electron IPC and writes them
 * into the runtimes Query cache for the active workspace.
 *
 * Why: the server-side runtime sweeper takes up to 75s to flip a runtime to
 * offline (heartbeat timeout 45s + sweep interval 30s). On the desktop app
 * we know about local daemon state instantly via IPC, so we use it to
 * pre-populate the cache and give users a sub-second feedback loop. Web and
 * "looking at someone else's daemon" still go through the server path.
 *
 * Same-daemon-multiple-runtimes: a single daemon can back several runtimes
 * in the same workspace (one per provider). We map across all matches so
 * every related runtime row sees the same status flip.
 */
export function useDaemonIPCBridge(wsId: string | undefined): void {
  const qc = useQueryClient();

  useEffect(() => {
    if (!wsId) return;
    if (typeof window === "undefined") return;
    const daemonAPI = (window as unknown as { daemonAPI?: { onStatusChange?: (cb: (s: DaemonStatusLike) => void) => () => void } }).daemonAPI;
    if (!daemonAPI?.onStatusChange) return;

    const unsubscribe = daemonAPI.onStatusChange((status) => {
      if (!status.daemonId) return;
      qc.setQueryData<AgentRuntime[]>(runtimeKeys.list(wsId), (old) => {
        if (!old) return old;
        return old.map((rt) =>
          rt.daemon_id === status.daemonId ? mergeDaemonStatus(rt, status) : rt,
        );
      });
    });

    return unsubscribe;
  }, [wsId, qc]);
}
