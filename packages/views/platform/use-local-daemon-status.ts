"use client";

import { useEffect, useState } from "react";

/** Subset of the daemonAPI status shape that the local_directory UI consumes.
 *  Redeclared here so this hook doesn't depend on the desktop preload types. */
export interface LocalDaemonStatus {
  daemonId: string | null;
  deviceName: string | null;
  running: boolean;
  /** Not running yet, but on its way: booting, installing its CLI, or
   *  restarting (Desktop stops and restarts it when the signed-in account
   *  changes). Its agent list is not meaningful until it is running. */
  starting: boolean;
  /** Provider slugs of the agent CLIs the daemon found on this machine. */
  agents: string[];
}

interface DaemonStatusLike {
  state:
    | "running"
    | "stopped"
    | "starting"
    | "stopping"
    | "installing_cli"
    | "cli_not_found"
    | "auth_expired";
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
}

interface DaemonAPILike {
  getStatus?: () => Promise<DaemonStatusLike>;
  onStatusChange?: (cb: (s: DaemonStatusLike) => void) => () => void;
  restart?: () => Promise<{ success: boolean; error?: string }>;
}

function readDaemonAPI(): DaemonAPILike | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { daemonAPI?: DaemonAPILike }).daemonAPI;
}

const STARTING_STATES = new Set<DaemonStatusLike["state"]>([
  "starting",
  "installing_cli",
  "stopping",
  "stopped",
]);

function toStatus(s: DaemonStatusLike | undefined): LocalDaemonStatus {
  if (!s) {
    return { daemonId: null, deviceName: null, running: false, starting: false, agents: [] };
  }
  return {
    daemonId: s.daemonId ?? null,
    deviceName: s.deviceName ?? null,
    running: s.state === "running",
    starting: STARTING_STATES.has(s.state),
    agents: Array.isArray(s.agents) ? s.agents : [],
  };
}

/**
 * Live snapshot of the desktop's local daemon: the daemon_id it registers
 * under, the OS device name, and whether the supervisor is currently running.
 *
 * On web (no `window.daemonAPI`) every field is null/false — components can
 * unconditionally call this hook and branch on `daemonId` to decide whether
 * a local_directory resource matches "this machine".
 *
 * The initial paint reads `getStatus()` once so the UI doesn't flash a
 * "no daemon" state while waiting for the first push from `onStatusChange`.
 */
export function useLocalDaemonStatus(): LocalDaemonStatus {
  const [status, setStatus] = useState<LocalDaemonStatus>(() => toStatus(undefined));

  useEffect(() => {
    const api = readDaemonAPI();
    if (!api) return;
    let cancelled = false;
    if (api.getStatus) {
      api.getStatus().then((s) => {
        if (!cancelled) setStatus(toStatus(s));
      }).catch(() => {
        // Ignore — onStatusChange will populate once the daemon comes up.
      });
    }
    const unsubscribe = api.onStatusChange?.((s) => {
      setStatus(toStatus(s));
    });
    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, []);

  return status;
}

/**
 * Restarts the desktop's bundled daemon so it scans PATH again — its agent
 * CLI probe runs once at boot, so a tool installed afterwards stays invisible
 * until then. Resolves false on web, where there is no bundled daemon.
 */
export async function restartLocalDaemon(): Promise<boolean> {
  const api = readDaemonAPI();
  if (!api?.restart) return false;
  const result = await api.restart();
  return result.success;
}
