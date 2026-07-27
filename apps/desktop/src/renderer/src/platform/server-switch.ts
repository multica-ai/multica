import {
  restoreServerSession,
  snapshotServerSession,
} from "../../../shared/server-session";

/**
 * Persist the current session, switch the active Multica backend, restore
 * the target server's session (or clear it so login is required), and reload
 * the renderer so CoreProvider boots against the new API/WS URLs.
 */
export async function switchDesktopServer(serverId: string): Promise<void> {
  const current = window.desktopAPI.runtimeConfig;
  if (!current.ok) {
    throw new Error(current.error.message);
  }

  // Flush live token/tabs into the current host namespace before leaving.
  snapshotServerSession(current.config.apiUrl);

  const result = await window.desktopAPI.switchServer(serverId);
  if (!result.ok) {
    throw new Error(result.error);
  }

  // Hydrate live keys for the destination backend before reload so auth
  // initialize() and tab persist pick up the right session.
  restoreServerSession(result.config.apiUrl);

  // Full reload: CoreProvider initCore is once-only and preload re-reads
  // runtime-config:get on the next document load.
  window.location.reload();
}
