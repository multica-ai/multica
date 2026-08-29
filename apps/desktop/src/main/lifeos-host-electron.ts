import {
  BrowserWindow,
  ipcMain,
  Menu,
  nativeImage,
  shell,
  Tray,
} from "electron";
import { homedir } from "node:os";
import { join } from "node:path";
import {
  LIFEOS_HOST_ENSURE_CHANNEL,
  LIFEOS_HOST_GET_STATUS_CHANNEL,
  LIFEOS_HOST_OPEN_LOGS_CHANNEL,
  LIFEOS_HOST_STATUS_CHANNEL,
  type LifeOSHostStatus,
} from "../shared/lifeos-host";
import { LifeOSHostController } from "./lifeos-host-controller";

const PROBE_INTERVAL_MS = 60_000;

export interface LifeOSHostIntegration {
  ensureOnLaunch: () => Promise<LifeOSHostStatus>;
  dispose: () => void;
}

export function setupLifeOSHost(options: {
  enabled: boolean;
  getMainWindow: () => BrowserWindow | null;
  showMainWindow: () => void;
  iconPath: string;
}): LifeOSHostIntegration {
  if (!options.enabled) {
    const disabledStatus: LifeOSHostStatus = {
      state: "disabled",
      checkedAt: Date.now(),
    };
    ipcMain.on(LIFEOS_HOST_GET_STATUS_CHANNEL, (event) => {
      event.returnValue = disabledStatus;
    });
    ipcMain.handle(LIFEOS_HOST_ENSURE_CHANNEL, async () => disabledStatus);
    ipcMain.handle(LIFEOS_HOST_OPEN_LOGS_CHANNEL, async () => ({ ok: false }));
    return {
      ensureOnLaunch: async () => disabledStatus,
      dispose: () => undefined,
    };
  }

  const controller = new LifeOSHostController();
  const trayIcon = nativeImage.createFromPath(options.iconPath).resize({
    width: 18,
    height: 18,
  });
  trayIcon.setTemplateImage(true);
  const tray = new Tray(trayIcon);
  let currentStatus = controller.getStatus();

  const showWindow = () => options.showMainWindow();
  const openLogs = async () => {
    const error = await shell.openPath(
      join(homedir(), "Library", "Logs", "LifeOS"),
    );
    return error ? { ok: false, error } : { ok: true };
  };
  const statusText = (status: LifeOSHostStatus): string => {
    switch (status.state) {
      case "ready":
        return "本机服务正常";
      case "starting":
        return "正在恢复本机服务…";
      case "offline":
        return "本机服务未连接";
      case "error":
        return "本机服务需要处理";
      default:
        return "本机服务状态未知";
    }
  };
  const refreshTray = (status: LifeOSHostStatus) => {
    currentStatus = status;
    tray.setToolTip(`LifeOS · ${statusText(status)}`);
    tray.setContextMenu(
      Menu.buildFromTemplate([
        { label: statusText(status), enabled: false },
        { type: "separator" },
        { label: "打开 LifeOS", click: showWindow },
        {
          label: status.state === "starting" ? "正在恢复…" : "恢复本机服务",
          enabled: status.state !== "starting",
          click: () => {
            void controller.ensure();
          },
        },
        {
          label: "打开运行日志",
          click: () => {
            void openLogs();
          },
        },
        { type: "separator" },
        { role: "quit", label: "退出 LifeOS" },
      ]),
    );
  };

  tray.on("click", showWindow);
  refreshTray(currentStatus);

  const unsubscribe = controller.subscribe((status) => {
    refreshTray(status);
    const window = options.getMainWindow();
    if (window && !window.isDestroyed()) {
      window.webContents.send(LIFEOS_HOST_STATUS_CHANNEL, status);
    }
  });

  ipcMain.on(LIFEOS_HOST_GET_STATUS_CHANNEL, (event) => {
    event.returnValue = currentStatus;
  });
  ipcMain.handle(LIFEOS_HOST_ENSURE_CHANNEL, async () => controller.ensure());
  ipcMain.handle(LIFEOS_HOST_OPEN_LOGS_CHANNEL, openLogs);

  const probeTimer = setInterval(() => {
    void controller.probe();
  }, PROBE_INTERVAL_MS);
  probeTimer.unref();

  return {
    ensureOnLaunch: () => controller.ensureIfNeeded(),
    dispose: () => {
      clearInterval(probeTimer);
      unsubscribe();
      tray.destroy();
    },
  };
}
