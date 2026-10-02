import { app, Menu, nativeImage, Tray } from "electron";

export interface TrayManagerOptions {
  iconPath: string;
  showWindow: () => void;
  requestQuit: () => void;
}

let tray: Tray | null = null;

type TrayMenuLabels = {
  open: string;
  quit: string;
};

const labelsByLocale: Record<string, TrayMenuLabels> = {
  en: { open: "Open Multica", quit: "Quit Multica" },
  "zh-Hans": { open: "打开 Multica", quit: "退出 Multica" },
  ja: { open: "Multica を開く", quit: "Multica を終了" },
  ko: { open: "Multica 열기", quit: "Multica 종료" },
};

export function pickTrayMenuLabels(preferredLanguage: string): TrayMenuLabels {
  const preferred = preferredLanguage.toLowerCase();
  if (preferred.startsWith("zh")) return labelsByLocale["zh-Hans"];
  if (preferred.startsWith("ja")) return labelsByLocale.ja;
  if (preferred.startsWith("ko")) return labelsByLocale.ko;
  return labelsByLocale.en;
}

export function buildTrayMenuTemplate(
  actions: {
    showWindow: () => void;
    requestQuit: () => void;
  },
  preferredLanguage = app.getPreferredSystemLanguages()[0] ?? "",
): Electron.MenuItemConstructorOptions[] {
  const labels = pickTrayMenuLabels(preferredLanguage);
  return [
    { label: labels.open, click: actions.showWindow },
    { type: "separator" },
    { label: labels.quit, click: actions.requestQuit },
  ];
}

export function setupTray(options: TrayManagerOptions): void {
  if (tray) return;

  const image = nativeImage.createFromPath(options.iconPath).resize({
    width: 18,
    height: 18,
  });

  tray = new Tray(image);
  tray.setToolTip("Multica");
  tray.setContextMenu(
    Menu.buildFromTemplate(
      buildTrayMenuTemplate({
        showWindow: options.showWindow,
        requestQuit: options.requestQuit,
      }),
    ),
  );
  tray.on("click", options.showWindow);
  tray.on("double-click", options.showWindow);

  app.on("before-quit", () => {
    tray?.destroy();
    tray = null;
  });
}

export function resetTrayForTests(): void {
  tray?.destroy();
  tray = null;
}
