import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import type { DaemonStatus } from "../shared/daemon-types";

// `vi.mock(...)` factories are hoisted above all `import` and `const`
// declarations in the file, so anything they reference must be hoisted with
// them. `vi.hoisted` is the supported escape hatch.
const h = vi.hoisted(() => {
  class MockTray {
    setImageMock = (globalThis as unknown as { vi: typeof vi }).vi.fn();
    setToolTipMock = (globalThis as unknown as { vi: typeof vi }).vi.fn();
    setContextMenuMock = (globalThis as unknown as { vi: typeof vi }).vi.fn();
    destroyMock = (globalThis as unknown as { vi: typeof vi }).vi.fn();
    clickListeners: Array<() => void> = [];
    setImage = this.setImageMock;
    setToolTip = this.setToolTipMock;
    setContextMenu = this.setContextMenuMock;
    destroy = this.destroyMock;
    on(event: string, listener: () => void): this {
      if (event === "click") this.clickListeners.push(listener);
      return this;
    }
  }
  return {
    MockTray,
    trayInstances: [] as InstanceType<typeof MockTray>[],
    subscribeListeners: [] as Array<(s: DaemonStatus) => void>,
    lastEmit: { value: { state: "stopped" } as DaemonStatus },
    appListeners: {} as Record<string, Array<(...a: unknown[]) => void>>,
  };
});

// Make `vi` available on globalThis so the hoisted factory above can reach
// the test runner's `vi.fn` without re-importing it (the factory runs before
// the top-level imports execute).
(globalThis as unknown as { vi: typeof vi }).vi = vi;

vi.mock("electron", () => {
  class TrayWrapper {
    constructor() {
      const inner = new h.MockTray();
      h.trayInstances.push(inner);
      return inner as unknown as TrayWrapper;
    }
  }
  return {
    app: {
      getAppPath: () => "/app",
      quit: vi.fn(),
      on: (event: string, listener: (...args: unknown[]) => void) => {
        (h.appListeners[event] ??= []).push(listener);
      },
    },
    BrowserWindow: class {},
    Tray: TrayWrapper,
    Menu: {
      buildFromTemplate: vi.fn((template) => ({ __template: template })),
    },
    nativeImage: {
      createFromPath: vi.fn((path: string) => ({ __path: path })),
    },
  };
});

vi.mock("./daemon-manager", () => ({
  subscribeDaemonStatus: (listener: (s: DaemonStatus) => void) => {
    h.subscribeListeners.push(listener);
    listener(h.lastEmit.value);
    return () => {
      const i = h.subscribeListeners.indexOf(listener);
      if (i >= 0) h.subscribeListeners.splice(i, 1);
    };
  },
  daemonOps: {
    start: vi.fn().mockResolvedValue({ success: true }),
    stop: vi.fn().mockResolvedValue({ success: true }),
    restart: vi.fn().mockResolvedValue({ success: true }),
  },
  openDaemonLogFile: vi.fn().mockResolvedValue({ success: true }),
}));

import {
  __resetTrayForTests,
  buildMenuTemplate,
  formatStatusLabel,
  setupTray,
} from "./tray-manager";
import { app, Menu, nativeImage } from "electron";
import { daemonOps, openDaemonLogFile } from "./daemon-manager";

function emit(status: DaemonStatus): void {
  h.lastEmit.value = status;
  for (const l of h.subscribeListeners) l(status);
}

function getLastMenu(): Electron.MenuItemConstructorOptions[] {
  const calls = (Menu.buildFromTemplate as unknown as { mock: { calls: unknown[][] } }).mock.calls;
  const last = calls[calls.length - 1]?.[0];
  return last as Electron.MenuItemConstructorOptions[];
}

function lastImagePath(): string | undefined {
  const calls = (nativeImage.createFromPath as unknown as {
    mock: { calls: [string][] };
  }).mock.calls;
  return calls.at(-1)?.[0];
}

beforeEach(() => {
  h.trayInstances.length = 0;
  h.subscribeListeners.length = 0;
  h.lastEmit.value = { state: "stopped" };
  for (const k of Object.keys(h.appListeners)) delete h.appListeners[k];
  vi.mocked(app.quit).mockClear();
  vi.mocked(daemonOps.start).mockClear();
  vi.mocked(daemonOps.stop).mockClear();
  vi.mocked(daemonOps.restart).mockClear();
  vi.mocked(openDaemonLogFile).mockClear();
  (Menu.buildFromTemplate as unknown as { mock: { calls: unknown[][] } }).mock.calls.length = 0;
  (nativeImage.createFromPath as unknown as { mock: { calls: unknown[][] } }).mock.calls.length = 0;
});

afterEach(() => {
  __resetTrayForTests();
});

describe("formatStatusLabel", () => {
  it("includes pid and agent count when running", () => {
    expect(
      formatStatusLabel({ state: "running", pid: 1234, agents: ["a", "b", "c"] }),
    ).toBe("Running · pid 1234 · 3 agents");
  });

  it("uses singular when one agent is registered", () => {
    expect(
      formatStatusLabel({ state: "running", pid: 7, agents: ["a"] }),
    ).toBe("Running · pid 7 · 1 agent");
  });

  it("omits agent count when zero", () => {
    expect(formatStatusLabel({ state: "running", pid: 1, agents: [] })).toBe(
      "Running · pid 1",
    );
  });

  it("covers transient states", () => {
    expect(formatStatusLabel({ state: "stopped" })).toBe("Stopped");
    expect(formatStatusLabel({ state: "starting" })).toBe("Starting…");
    expect(formatStatusLabel({ state: "stopping" })).toBe("Stopping…");
    expect(formatStatusLabel({ state: "installing_cli" })).toBe("Setting up…");
    expect(formatStatusLabel({ state: "cli_not_found" })).toBe("Setup failed");
  });
});

describe("buildMenuTemplate", () => {
  const noop = () => {};
  const actions = {
    showWindow: noop,
    openLog: noop,
    start: noop,
    stop: noop,
    restart: noop,
    quit: noop,
  };

  it("disables Start and enables Stop/Restart when running", () => {
    const t = buildMenuTemplate({ state: "running", pid: 1 }, actions);
    const byLabel = Object.fromEntries(
      t.filter((i) => i.label).map((i) => [i.label, i]),
    );
    expect(byLabel["Start Daemon"]?.enabled).toBe(false);
    expect(byLabel["Stop Daemon"]?.enabled).toBe(true);
    expect(byLabel["Restart Daemon"]?.enabled).toBe(true);
  });

  it("enables Start only when stopped or cli_not_found", () => {
    for (const state of ["stopped", "cli_not_found"] as const) {
      const t = buildMenuTemplate({ state }, actions);
      const start = t.find((i) => i.label === "Start Daemon");
      expect(start?.enabled).toBe(true);
    }
  });

  it("disables every daemon action while transitioning", () => {
    for (const state of ["starting", "stopping", "installing_cli"] as const) {
      const t = buildMenuTemplate({ state }, actions);
      const byLabel = Object.fromEntries(
        t.filter((i) => i.label).map((i) => [i.label, i]),
      );
      expect(byLabel["Start Daemon"]?.enabled).toBe(false);
      expect(byLabel["Stop Daemon"]?.enabled).toBe(false);
      expect(byLabel["Restart Daemon"]?.enabled).toBe(false);
    }
  });

  it("places the status label as a disabled first row", () => {
    const t = buildMenuTemplate({ state: "stopped" }, actions);
    expect(t[0]).toMatchObject({ label: "Stopped", enabled: false });
  });
});

describe("setupTray", () => {
  it("creates a Tray once and ignores duplicate setupTray calls", () => {
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    expect(h.trayInstances).toHaveLength(1);
  });

  it("Show Multica menu item invokes showOrCreateWindow", () => {
    const showOrCreateWindow = vi.fn();
    setupTray({ getWindow: () => null, showOrCreateWindow });
    const menu = getLastMenu();
    const item = menu.find((i) => i.label === "Show Multica");
    (item?.click as () => void)?.();
    expect(showOrCreateWindow).toHaveBeenCalledTimes(1);
  });

  it("tray click on darwin recreates the window when getWindow returns null", () => {
    const orig = Object.getOwnPropertyDescriptor(process, "platform");
    Object.defineProperty(process, "platform", { value: "darwin" });
    try {
      const showOrCreateWindow = vi.fn();
      setupTray({ getWindow: () => null, showOrCreateWindow });
      const click = h.trayInstances[0]!.clickListeners[0]!;
      click();
      expect(showOrCreateWindow).toHaveBeenCalledTimes(1);
    } finally {
      if (orig) Object.defineProperty(process, "platform", orig);
    }
  });

  it("tray click on darwin treats a destroyed BrowserWindow as missing", () => {
    const orig = Object.getOwnPropertyDescriptor(process, "platform");
    Object.defineProperty(process, "platform", { value: "darwin" });
    try {
      const showOrCreateWindow = vi.fn();
      const destroyedWindow = {
        isDestroyed: () => true,
        isVisible: () => true,
        isMinimized: () => false,
        hide: vi.fn(),
        show: vi.fn(),
        focus: vi.fn(),
        restore: vi.fn(),
      } as unknown as Electron.BrowserWindow;
      setupTray({ getWindow: () => destroyedWindow, showOrCreateWindow });
      const click = h.trayInstances[0]!.clickListeners[0]!;
      click();
      expect(showOrCreateWindow).toHaveBeenCalledTimes(1);
      expect(destroyedWindow.hide).not.toHaveBeenCalled();
    } finally {
      if (orig) Object.defineProperty(process, "platform", orig);
    }
  });

  it("tray click on darwin always shows + focuses a visible window, never hides it", () => {
    const orig = Object.getOwnPropertyDescriptor(process, "platform");
    Object.defineProperty(process, "platform", { value: "darwin" });
    try {
      const showOrCreateWindow = vi.fn();
      const visibleWindow = {
        isDestroyed: () => false,
        isVisible: () => true,
        isMinimized: () => false,
        hide: vi.fn(),
        show: vi.fn(),
        focus: vi.fn(),
        restore: vi.fn(),
      } as unknown as Electron.BrowserWindow;
      setupTray({ getWindow: () => visibleWindow, showOrCreateWindow });
      const click = h.trayInstances[0]!.clickListeners[0]!;
      click();
      click();
      click();
      expect(showOrCreateWindow).toHaveBeenCalledTimes(3);
      expect(visibleWindow.hide).not.toHaveBeenCalled();
    } finally {
      if (orig) Object.defineProperty(process, "platform", orig);
    }
  });

  it("replays current status on subscribe and renders an initial menu", () => {
    h.lastEmit.value = { state: "running", pid: 99, agents: ["a"] };
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    const menu = getLastMenu();
    expect(menu[0]).toMatchObject({
      label: "Running · pid 99 · 1 agent",
      enabled: false,
    });
  });

  it("swaps the tray image and rebuilds the menu on status change", () => {
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    const tray = h.trayInstances[0]!;
    tray.setImageMock.mockClear();
    (Menu.buildFromTemplate as unknown as { mock: { calls: unknown[][] } }).mock.calls.length = 0;

    emit({ state: "running", pid: 12 });

    expect(tray.setImageMock).toHaveBeenCalledTimes(1);
    expect(lastImagePath()).toMatch(/tray-running(-Template)?\.png$/);

    const menu = getLastMenu();
    expect(menu[0]).toMatchObject({ label: "Running · pid 12", enabled: false });
  });

  it("maps installing_cli and stopping to the starting silhouette", () => {
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    const tray = h.trayInstances[0]!;

    emit({ state: "installing_cli" });
    expect(lastImagePath()).toMatch(/tray-starting(-Template)?\.png$/);

    emit({ state: "stopping" });
    expect(lastImagePath()).toMatch(/tray-starting(-Template)?\.png$/);

    expect(tray.setImageMock).toHaveBeenCalled();
  });

  it("maps cli_not_found to the error silhouette", () => {
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    emit({ state: "cli_not_found" });
    expect(lastImagePath()).toMatch(/tray-error(-Template)?\.png$/);
  });

  it("wires menu clicks to daemonOps and openDaemonLogFile", () => {
    h.lastEmit.value = { state: "running", pid: 1 };
    setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
    const menu = getLastMenu();
    const click = (label: string): void => {
      const item = menu.find((i) => i.label === label);
      (item?.click as () => void)?.();
    };

    click("Stop Daemon");
    click("Restart Daemon");
    click("Open Log File");
    click("Quit Multica");

    expect(daemonOps.stop).toHaveBeenCalledTimes(1);
    expect(daemonOps.restart).toHaveBeenCalledTimes(1);
    expect(openDaemonLogFile).toHaveBeenCalledTimes(1);
    expect(app.quit).toHaveBeenCalledTimes(1);
  });

  it("does not register a click listener on Linux", () => {
    const orig = Object.getOwnPropertyDescriptor(process, "platform");
    Object.defineProperty(process, "platform", { value: "linux" });
    try {
      setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
      expect(h.trayInstances[0]!.clickListeners).toHaveLength(0);
    } finally {
      if (orig) Object.defineProperty(process, "platform", orig);
    }
  });

  it("registers a click listener on darwin", () => {
    const orig = Object.getOwnPropertyDescriptor(process, "platform");
    Object.defineProperty(process, "platform", { value: "darwin" });
    try {
      setupTray({ getWindow: () => null, showOrCreateWindow: vi.fn() });
      expect(h.trayInstances[0]!.clickListeners).toHaveLength(1);
    } finally {
      if (orig) Object.defineProperty(process, "platform", orig);
    }
  });
});
