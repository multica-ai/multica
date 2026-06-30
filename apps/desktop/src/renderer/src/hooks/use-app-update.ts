import { useEffect, useState } from "react";

export type AppUpdateState = {
  /** Newest version the background updater has found. */
  version: string;
  /**
   * True once the package is fully downloaded and a restart will apply it.
   * False while it is still downloading in the background.
   */
  downloaded: boolean;
};

/**
 * Tracks whether the background auto-updater has found a newer version, so the
 * top-bar update affordance can stay hidden while the app is up to date and
 * only appear when there is something to install.
 *
 * Returns `null` when the app is up to date (caller renders nothing).
 *
 * The main process already checks on a schedule — 5s after boot and hourly
 * thereafter, see `src/main/updater.ts`. This hook mirrors that background
 * result into the renderer by:
 *   1. asking once on mount for the authoritative current verdict, which
 *      covers a check that completed before this hook subscribed, and
 *   2. listening for the live `update-available` / `update-downloaded`
 *      events so long-running sessions surface new releases without a reload.
 */
export function useAppUpdate(): AppUpdateState | null {
  const [update, setUpdate] = useState<AppUpdateState | null>(null);

  useEffect(() => {
    let cancelled = false;

    // Point-in-time check. `available` is electron-updater's own verdict — it
    // accounts for pre-release channels, staged rollouts, downgrades and
    // minimum-system-version gates — so we trust it instead of diffing version
    // strings here (see the rationale in src/main/updater.ts).
    void window.updater
      .checkForUpdates()
      .then((result) => {
        if (cancelled || !result.ok || !result.available) return;
        // Don't clobber a download-complete state if the event raced ahead.
        setUpdate((prev) => prev ?? { version: result.latestVersion, downloaded: false });
      })
      .catch(() => {
        // A failed check just means "nothing to show" for now; the periodic
        // background check and the listeners below will correct this later.
      });

    const offAvailable = window.updater.onUpdateAvailable((info) => {
      // Same version → keep whatever download state we already have. A genuinely
      // newer version → reset to "not downloaded yet".
      setUpdate((prev) =>
        prev && prev.version === info.version
          ? prev
          : { version: info.version, downloaded: false },
      );
    });
    const offDownloaded = window.updater.onUpdateDownloaded((info) => {
      setUpdate({ version: info.version, downloaded: true });
    });

    return () => {
      cancelled = true;
      offAvailable();
      offDownloaded();
    };
  }, []);

  return update;
}
