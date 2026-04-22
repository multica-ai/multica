import { autoUpdater } from "electron-updater";
import { app, BrowserWindow, ipcMain } from "electron";

autoUpdater.autoDownload = false;
autoUpdater.autoInstallOnAppQuit = true;

// Windows arm64 ships its own update metadata channel because
// electron-builder's `latest.yml` is not arch-suffixed on Windows — both
// arches would otherwise collide on the same file in the GitHub Release.
// See scripts/package.mjs (builderArgsForTarget) for the publish-side half
// of this pact. Pin the channel here so arm64 clients fetch
// `latest-arm64.yml` instead of the x64 metadata.
if (process.platform === "win32" && process.arch === "arm64") {
  autoUpdater.channel = "latest-arm64";
}

const STARTUP_CHECK_DELAY_MS = 5_000;
const PERIODIC_CHECK_INTERVAL_MS = 60 * 60 * 1000; // 1 hour

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
    }
  | { ok: false; error: string };

export function setupAutoUpdater(getMainWindow: () => BrowserWindow | null): void {
  autoUpdater.on("update-available", (info) => {
    const win = getMainWindow();
    win?.webContents.send("updater:update-available", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("download-progress", (progress) => {
    const win = getMainWindow();
    win?.webContents.send("updater:download-progress", {
      percent: progress.percent,
    });
  });

  autoUpdater.on("update-downloaded", () => {
    const win = getMainWindow();
    win?.webContents.send("updater:update-downloaded");
  });

  autoUpdater.on("error", (err) => {
    console.error("Auto-updater error:", err);
  });

  ipcMain.handle("updater:download", () => {
    return autoUpdater.downloadUpdate();
  });

  ipcMain.handle("updater:install", () => {
    autoUpdater.quitAndInstall(false, true);
  });

  ipcMain.handle("updater:check", async (): Promise<ManualUpdateCheckResult> => {
    try {
      const result = await autoUpdater.checkForUpdates();
      const currentVersion = app.getVersion();
      // Trust electron-updater's own decision rather than re-deriving it from
      // a version-string compare. The two diverge for pre-release channels,
      // staged rollouts, downgrades, and minimum-system-version gates — in
      // those cases updateInfo.version differs from app.getVersion() but no
      // `update-available` event fires, so showing "available" here would
      // promise a download prompt that never appears.
      return {
        ok: true,
        currentVersion,
        latestVersion: result?.updateInfo.version ?? currentVersion,
        available: result?.isUpdateAvailable ?? false,
      };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : String(err),
      };
    }
  });

  // Initial check shortly after startup so we don't block boot.
  setTimeout(() => {
    autoUpdater.checkForUpdates().catch((err) => {
      console.error("Failed to check for updates:", err);
    });
  }, STARTUP_CHECK_DELAY_MS);

  // Background poll so long-running sessions still pick up new releases
  // without requiring the user to restart the app.
  setInterval(() => {
    autoUpdater.checkForUpdates().catch((err) => {
      console.error("Periodic update check failed:", err);
    });
  }, PERIODIC_CHECK_INTERVAL_MS);
}
