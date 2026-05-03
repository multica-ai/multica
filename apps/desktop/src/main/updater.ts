import { autoUpdater } from "electron-updater";
import { app, BrowserWindow, ipcMain } from "electron";
import {
  customInstallMacApp,
  scheduleMacFallbackInstall,
} from "./mac-update-installer";

autoUpdater.autoDownload = false;
autoUpdater.autoInstallOnAppQuit = false;

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
const MAC_STANDARD_INSTALL_GRACE_MS = 10_000;

let latestDownloadedFile: string | null = null;

function logUpdater(message: string, extra?: unknown): void {
  if (extra === undefined) {
    console.log(`[updater] ${message}`);
  } else {
    console.log(`[updater] ${message}`, extra);
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
    }
  | { ok: false; error: string };

export function setupAutoUpdater(getMainWindow: () => BrowserWindow | null): void {
  autoUpdater.on("checking-for-update", () => {
    logUpdater("checking for update");
  });

  autoUpdater.on("update-available", (info) => {
    logUpdater("update available", {
      version: info.version,
    });
    const win = getMainWindow();
    win?.webContents.send("updater:update-available", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("download-progress", (progress) => {
    logUpdater("download progress", {
      percent: Math.round(progress.percent),
    });
    const win = getMainWindow();
    win?.webContents.send("updater:download-progress", {
      percent: progress.percent,
    });
  });

  autoUpdater.on("update-downloaded", (event) => {
    latestDownloadedFile =
      typeof event?.downloadedFile === "string" ? event.downloadedFile : null;
    logUpdater("update downloaded", {
      version: event?.version,
      downloadedFile: latestDownloadedFile,
    });
    const win = getMainWindow();
    win?.webContents.send("updater:update-downloaded");
  });

  autoUpdater.on("error", (err) => {
    console.error("[updater] auto-updater error:", err);
  });

  ipcMain.handle("updater:download", async () => {
    const downloadedFiles = await autoUpdater.downloadUpdate();
    if (downloadedFiles[0]) {
      latestDownloadedFile = downloadedFiles[0];
      logUpdater("downloadUpdate resolved", {
        downloadedFile: latestDownloadedFile,
      });
    }
    return downloadedFiles;
  });

  ipcMain.handle("updater:install", () => {
    logUpdater("install requested", {
      platform: process.platform,
      latestDownloadedFile,
    });
    autoUpdater.quitAndInstall(false, true);
    logUpdater("quitAndInstall returned");

    if (process.platform !== "darwin") {
      return { ok: true };
    }

    scheduleMacFallbackInstall({
      app,
      delayMs: MAC_STANDARD_INSTALL_GRACE_MS,
      fallback: () => {
        if (!latestDownloadedFile) {
          console.error("[updater] mac fallback skipped: no downloaded file");
          return;
        }

        logUpdater("standard mac installer did not quit, starting fallback", {
          downloadedFile: latestDownloadedFile,
        });
        void customInstallMacApp(latestDownloadedFile).then((result) => {
          if (!result.ok) {
            console.error("[updater] mac fallback failed:", result.error);
            return;
          }

          logUpdater("mac fallback installer started", {
            scriptPath: result.scriptPath,
            logPath: result.logPath,
          });
          app.quit();
        });
      },
    });

    return { ok: true };
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
        error: errorMessage(err),
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
