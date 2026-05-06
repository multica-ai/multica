import { app } from "electron";
import path from "path";

/**
 * Filesystem locations Multica owns under the user-data directory. Pure
 * read-only path resolution — no I/O, no mkdir, no existsSync. The supervisor
 * + managed-postgres modules are responsible for creating these directories
 * lazily when they actually write data.
 *
 * `appLogs` resolves via `app.getPath("logs")` because Electron points it at
 * the platform-specific log directory (~/Library/Logs/<app> on macOS,
 * %USERPROFILE%/AppData/Roaming/<app>/logs on Windows). Surfacing it here
 * lets the diagnostics UI offer "open the OS log folder" without repeating
 * the platform branching across the codebase.
 */
export type LocalDataPaths = {
  /** Root user-data directory Electron owns for this app. */
  root: string;
  /** Postgres cluster data dir. */
  postgresData: string;
  /** Postgres logs (managed-postgres redirects pg_ctl logs here). */
  postgresLogs: string;
  /** Daemon log root. */
  daemonLogs: string;
  /** App-process logs (electron + main). */
  appLogs: string;
  /** Electron persisted state (cookies, localStorage). */
  appConfig: string;
};

export function resolveLocalDataPaths(): LocalDataPaths {
  const root = app.getPath("userData");
  return {
    root,
    postgresData: path.join(root, "postgres", "data"),
    postgresLogs: path.join(root, "postgres", "logs"),
    daemonLogs: path.join(root, "daemon", "logs"),
    appLogs: app.getPath("logs"),
    appConfig: root,
  };
}
