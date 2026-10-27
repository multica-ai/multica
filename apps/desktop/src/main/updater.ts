import { autoUpdater, type UpdateDownloadedEvent } from "electron-updater";
import { app, type BrowserWindow, type MessageBoxOptions, ipcMain } from "electron";
import type {
  ManualUpdateCheckResult,
  UpdaterPreferences,
} from "../shared/updater-types";
import {
  DEFAULT_UPDATER_PREFERENCES,
  loadUpdaterPreferences,
  saveUpdaterPreferences,
  updaterPreferencesPath,
} from "./updater-preferences";
import type { DaemonInstallPrep } from "./update-install-gate";

// Silent background updates: electron-updater downloads on its own as soon
// as `update-available` fires; we only surface UI when the package is fully
// downloaded and ready to install on next quit.
autoUpdater.autoDownload = true;
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

interface ChannelConfigurableUpdater {
  channel: string | null;
  allowDowngrade: boolean;
}

export function configureMacX64UpdateChannel(
  updater: ChannelConfigurableUpdater,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): void {
  if (platform !== "darwin" || arch !== "x64") return;

  // AppUpdater.channel enables allowDowngrade as a side effect. This channel
  // isolates a CPU architecture, not a release train, so preserve normal
  // monotonic version behavior after selecting the architecture feed.
  updater.channel = "latest-x64";
  updater.allowDowngrade = false;
}

// electron-builder does not architecture-suffix macOS update metadata.
// package.mjs publishes macOS x64 as `latest-x64-mac.yml`; the established
// arm64 feed and runtime path remain unchanged.
configureMacX64UpdateChannel(autoUpdater);

const STARTUP_CHECK_DELAY_MS = 5_000;
const PERIODIC_CHECK_INTERVAL_MS = 60 * 60 * 1000; // 1 hour
// quitAndInstall only quits once the installer has been spawned. If the app is
// still running this long afterwards, the install bailed and the daemon we
// stopped for it must be brought back.
const INSTALL_QUIT_WATCHDOG_MS = 10_000;

// Injected so the updater does not own the daemon manager's state.
export interface UpdaterDaemonGate {
  prepare: (options: {
    interruptActiveTasks: boolean;
  }) => Promise<DaemonInstallPrep>;
  resumeAfterAbortedInstall: () => void;
  confirmInstallWhileBusy: (activeTasks: number) => Promise<boolean>;
}

export interface SetupAutoUpdaterOptions {
  daemonGate?: UpdaterDaemonGate;
  platform?: NodeJS.Platform;
}

export function createElectronBusyInstallPrompt(
  showMessageBox: (
    options: MessageBoxOptions,
  ) => Promise<{ response: number }>,
): (activeTasks: number) => Promise<boolean> {
  return async (activeTasks) => {
    const tasks = activeTasks === 1 ? "1 agent task is" : `${activeTasks} agent tasks are`;
    const { response } = await showMessageBox({
      type: "question",
      buttons: ["Install now", "Cancel"],
      defaultId: 1,
      cancelId: 1,
      title: "Install update",
      message: `${tasks} running on this computer.`,
      detail: "Installing now stops the local runtime and interrupts these tasks.",
    });
    return response === 0;
  };
}

type RendererChannel =
  | "updater:update-available"
  | "updater:download-progress"
  | "updater:update-downloaded";

function isDestroyedObjectError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("Object has been destroyed");
}

function sendToLiveRenderer(
  win: BrowserWindow | null,
  channel: RendererChannel,
  payload: unknown,
): void {
  if (!win || win.isDestroyed()) return;

  try {
    const { webContents } = win;
    if (webContents.isDestroyed()) return;
    webContents.send(channel, payload);
  } catch (err) {
    if (isDestroyedObjectError(err)) return;
    throw err;
  }
}

// Single-flight guard around checkForUpdates(). With autoDownload=true the
// startup, periodic, and manual triggers can all kick off downloads, and
// overlapping calls have caused duplicate download warnings in the past
// (see electronjs.org/docs/latest/api/auto-updater). Coalesce concurrent
// callers onto the same in-flight promise.
let inFlightCheck: Promise<unknown> | null = null;
function checkForUpdatesOnce(): Promise<unknown> {
  if (inFlightCheck) return inFlightCheck;
  const p = autoUpdater
    .checkForUpdates()
    .then((result) => {
      // checkForUpdates resolves as soon as metadata is fetched; the actual
      // download (when autoDownload=true) is exposed on result.downloadPromise.
      // Without a handler a download failure becomes an unhandled rejection
      // in the main process — Node may terminate it on future versions.
      void (result as { downloadPromise?: Promise<unknown> } | null)?.downloadPromise?.catch(
        (err) => {
          console.error("Failed to download update:", err);
        },
      );
      return result;
    })
    .finally(() => {
      if (inFlightCheck === p) inFlightCheck = null;
    });
  inFlightCheck = p;
  return p;
}

export function setupAutoUpdater(
  getMainWindow: () => BrowserWindow | null,
  options: SetupAutoUpdaterOptions = {},
): void {
  const platform = options.platform ?? process.platform;
  // Only Windows locks a running executable against replacement; macOS and
  // Linux keep their existing update path untouched.
  const daemonGate = platform === "win32" ? (options.daemonGate ?? null) : null;
  const preferencesFilePath = updaterPreferencesPath(app.getPath("userData"));
  let automaticUpdatesEnabled =
    DEFAULT_UPDATER_PREFERENCES.automaticUpdates;
  let startupCheckElapsed = false;
  let startupTimer: ReturnType<typeof setTimeout> | null = null;
  let periodicTimer: ReturnType<typeof setInterval> | null = null;
  const preferencesReady = loadUpdaterPreferences(preferencesFilePath).then(
    (preferences) => {
      automaticUpdatesEnabled = preferences.automaticUpdates;
      return preferences;
    },
  );

  const runAutomaticCheck = (errorMessage: string): void => {
    void preferencesReady
      .then(() => {
        if (!automaticUpdatesEnabled) return;
        return checkForUpdatesOnce();
      })
      .catch((err) => {
        console.error(errorMessage, err);
      });
  };

  // Arm the startup + periodic background checks. Idempotent: an already-armed
  // timer is left in place so re-enabling never stacks duplicate schedules.
  const scheduleBackgroundChecks = (): void => {
    if (startupTimer === null && !startupCheckElapsed) {
      // Initial check shortly after startup so we don't block boot.
      startupTimer = setTimeout(() => {
        startupTimer = null;
        startupCheckElapsed = true;
        runAutomaticCheck("Failed to check for updates:");
      }, STARTUP_CHECK_DELAY_MS);
    }
    if (periodicTimer === null) {
      // Background poll so long-running sessions still pick up new releases
      // without requiring the user to restart the app.
      periodicTimer = setInterval(() => {
        runAutomaticCheck("Periodic update check failed:");
      }, PERIODIC_CHECK_INTERVAL_MS);
    }
  };

  // Tear down the scheduled checks outright when automatic updates are turned
  // off. Relying only on an in-callback preference guard leaves the timers
  // running and lets a tick that races the preference flip still fire a check;
  // clearing them makes "disabled" mean no future background work, full stop.
  const cancelBackgroundChecks = (): void => {
    if (startupTimer !== null) {
      clearTimeout(startupTimer);
      startupTimer = null;
    }
    if (periodicTimer !== null) {
      clearInterval(periodicTimer);
      periodicTimer = null;
    }
  };

  let updateReadyToInstall = false;
  let installFlow: Promise<void> | null = null;
  let quitRequested = false;
  let finishing = false;
  let appQuitting = false;

  const finish = (install: boolean, manual: boolean): void => {
    finishing = true;
    const previousAutoInstall = autoUpdater.autoInstallOnAppQuit;
    if (!install) autoUpdater.autoInstallOnAppQuit = false;
    setTimeout(() => {
      if (appQuitting) return;
      finishing = false;
      quitRequested = false;
      autoUpdater.autoInstallOnAppQuit = previousAutoInstall;
      daemonGate?.resumeAfterAbortedInstall();
    }, INSTALL_QUIT_WATCHDOG_MS);
    if (install && manual) autoUpdater.quitAndInstall(false, true);
    else app.quit();
  };

  // All entry points join this promise. Once stopping starts, nobody cancels
  // or replaces its PID wait, and no other flow can resume the daemon.
  const installWithDaemonGate = (manual: boolean): Promise<void> => {
    if (installFlow) return installFlow;
    if (finishing || !daemonGate) return Promise.resolve();
    installFlow = (async () => {
      let install = true;
      try {
        const prep = await daemonGate.prepare({ interruptActiveTasks: false });
        if (prep.kind === "busy") {
          install = manual && await daemonGate.confirmInstallWhileBusy(prep.activeTasks);
          if (install) await daemonGate.prepare({ interruptActiveTasks: true });
        }
      } catch (err) {
        // Preserve the existing installer fallback if preparation fails.
        console.error("[updater] daemon preparation failed; installing anyway:", err);
      }
      if (install) finish(true, manual);
      else {
        daemonGate.resumeAfterAbortedInstall();
        if (quitRequested) finish(false, false);
      }
    })().finally(() => { installFlow = null; });
    return installFlow;
  };

  if (daemonGate) {
    app.on("will-quit", () => { appQuitting = true; });
    app.on("before-quit", (event) => {
      if (finishing || !updateReadyToInstall || !autoUpdater.autoInstallOnAppQuit) return;
      event.preventDefault();
      quitRequested = true;
      void installWithDaemonGate(false).catch((err) => {
        console.error("[updater] quit-time installation failed:", err);
      });
    });
  }

  autoUpdater.on("update-available", (info) => {
    // Forwarded for renderer-side state tracking only; the notification UI
    // does not render an "available" affordance with autoDownload=true.
    sendToLiveRenderer(getMainWindow(), "updater:update-available", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("download-progress", (progress) => {
    sendToLiveRenderer(getMainWindow(), "updater:download-progress", {
      percent: progress.percent,
    });
  });

  autoUpdater.on("update-downloaded", (info: UpdateDownloadedEvent) => {
    updateReadyToInstall = true;
    sendToLiveRenderer(getMainWindow(), "updater:update-downloaded", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("error", (err) => {
    console.error("Auto-updater error:", err);
  });

  // Retained for IPC back-compat with older renderer bundles. With
  // autoDownload=true the renderer no longer triggers this path.
  ipcMain.handle("updater:download", () => {
    return autoUpdater.downloadUpdate();
  });

  ipcMain.handle("updater:install", async () => {
    if (!daemonGate) {
      autoUpdater.quitAndInstall(false, true);
      return;
    }
    await installWithDaemonGate(true);
  });

  ipcMain.handle(
    "updater:get-preferences",
    async (): Promise<UpdaterPreferences> => {
      await preferencesReady;
      return { automaticUpdates: automaticUpdatesEnabled };
    },
  );

  ipcMain.handle(
    "updater:set-automatic-updates",
    async (_event, enabled: unknown): Promise<UpdaterPreferences> => {
      if (typeof enabled !== "boolean") {
        throw new TypeError("automaticUpdates must be a boolean");
      }

      await preferencesReady;
      const wasEnabled = automaticUpdatesEnabled;
      const preferences = { automaticUpdates: enabled };
      await saveUpdaterPreferences(preferencesFilePath, preferences);
      automaticUpdatesEnabled = enabled;

      if (!enabled) {
        cancelBackgroundChecks();
      } else if (!wasEnabled) {
        // If the startup check has already passed while the preference was off,
        // enabling it should take effect now instead of waiting up to one hour.
        if (startupCheckElapsed) {
          runAutomaticCheck("Failed to check for updates:");
        }
        scheduleBackgroundChecks();
      }

      return preferences;
    },
  );

  ipcMain.handle("updater:check", async (): Promise<ManualUpdateCheckResult> => {
    try {
      const result = (await checkForUpdatesOnce()) as
        | { updateInfo: { version: string }; isUpdateAvailable?: boolean }
        | null;
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

  // Initial check shortly after startup so we don't block boot, plus a
  // background poll for long-running sessions. Both are torn down when the
  // user disables automatic updates and re-armed when they turn them back on.
  scheduleBackgroundChecks();
}
