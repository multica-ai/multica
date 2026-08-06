export interface DaemonIdentityHealth {
  status?: string;
  daemon_id?: string;
  server_url?: string;
  pid?: number;
  active_task_count?: number;
}

export interface LegacyDaemonConflict {
  pid?: number;
  activeTaskCount: number;
}

function normalizeUrl(value: string): string {
  try {
    const parsed = new URL(value);
    return `${parsed.protocol}//${parsed.host}`.toLowerCase();
  } catch {
    return value.replace(/\/+$/, "").toLowerCase();
  }
}

/**
 * Detects the migration hazard where an old default-profile daemon survived a
 * Desktop update and a new Desktop-owned profile is about to start alongside
 * it. Both processes use the machine-wide daemon.id and would therefore claim
 * tasks for the same runtime registrations.
 *
 * Be deliberately narrow: only a live default-port daemon with the confirmed
 * local machine identity and the same backend conflicts. A reused port, a
 * different machine identity, or an intentionally separate backend is not
 * affected.
 */
export function legacyDaemonConflict(
  localDaemonId: string | null,
  targetServerUrl: string | null,
  health: DaemonIdentityHealth | null,
): LegacyDaemonConflict | null {
  if (
    !localDaemonId ||
    !targetServerUrl ||
    !health ||
    (health.status !== "running" && health.status !== "starting") ||
    health.daemon_id !== localDaemonId ||
    !health.server_url ||
    normalizeUrl(health.server_url) !== normalizeUrl(targetServerUrl)
  ) {
    return null;
  }

  return {
    pid: health.pid,
    activeTaskCount: health.active_task_count ?? 0,
  };
}
