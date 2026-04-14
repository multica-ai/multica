import { autoUpdater } from "electron-updater";
import { BrowserWindow, ipcMain } from "electron";

autoUpdater.autoDownload = false;
autoUpdater.autoInstallOnAppQuit = true;

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

  // Check for updates after a short delay to avoid blocking startup
  setTimeout(() => {
    autoUpdater.checkForUpdates().catch((err) => {
      console.error("Failed to check for updates:", err);
    });
  }, 5000);
}
