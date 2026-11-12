import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from "@testing-library/react";
import {
  beforeAll,
  afterAll,
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";
import { DaemonSettingsTab } from "./daemon-settings-tab";
import { DaemonPanel } from "./daemon-panel";
import { readZip } from "@multica/core/appearance/theme-archive";
import { toast } from "sonner";
import { NavigationProvider } from "@multica/views/navigation";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { ApiClient } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";

const fixture = vi.hoisted(() => ({
  root: "",
  sender: {},
  handlers: new Map<string, (...args: any[]) => any>(),
  listeners: new Map<string, (...args: any[]) => any>(),
  rendererListeners: new Map<string, (...args: any[]) => any>(),
  intervals: [] as ReturnType<typeof setInterval>[],
  exec: vi.fn(),
  install: vi.fn(),
  save: vi.fn(),
  health: {
    status: "running",
    pid: 1234,
    uptime: "12m",
    daemon_id: "fixture-daemon",
    device_name: "fixture-device",
    server_url: "https://diagnostics.invalid",
    workspaces: [],
  },
}));

vi.mock("electron", () => ({
  app: {
    getAppPath: () => fixture.root,
    getVersion: () => "0.8.2-fixture",
    getPreferredSystemLanguages: () => ["en"],
    on: vi.fn(),
  },
  dialog: { showSaveDialog: fixture.save },
  shell: { openPath: vi.fn() },
  BrowserWindow: {
    fromWebContents: () => ({
      isDestroyed: () => false,
      webContents: {
        send: (channel: string, value: unknown) =>
          fixture.rendererListeners.get(channel)?.({}, value),
      },
    }),
    getAllWindows: () => [],
  },
  ipcMain: {
    handle: (name: string, fn: (...args: any[]) => any) =>
      fixture.handlers.set(name, fn),
    on: (name: string, fn: (...args: any[]) => any) =>
      fixture.listeners.set(name, fn),
  },
  ipcRenderer: {
    invoke: (name: string, ...args: unknown[]) =>
      Promise.resolve(
        fixture.handlers.get(name)!({ sender: fixture.sender }, ...args),
      ),
    send: (name: string, ...args: unknown[]) =>
      fixture.listeners.get(name)?.({ sender: fixture.sender }, ...args),
    sendSync: () => undefined,
    on: (name: string, fn: (...args: any[]) => any) =>
      fixture.rendererListeners.set(name, fn),
    removeListener: (name: string) => fixture.rendererListeners.delete(name),
  },
  contextBridge: {
    exposeInMainWorld: (name: string, value: unknown) =>
      Object.defineProperty(window, name, { configurable: true, value }),
  },
}));
vi.mock("@electron-toolkit/preload", () => ({ electronAPI: {} }));
vi.mock("child_process", () => ({
  default: { execFile: fixture.exec },
  execFile: fixture.exec,
}));
vi.mock("../../../main/cli-bootstrap", () => ({
  ensureManagedCli: fixture.install,
  managedCliPath: () => `${fixture.root}/managed-cli`,
}));
vi.mock("../../../main/daemon-port", () => ({
  probeProfileHealth: async () => fixture.health,
  readDaemonPortRecord: async () => null,
}));
vi.mock("../../../main/daemon-profile", async (original) => ({
  ...(await original<typeof import("../../../main/daemon-profile")>()),
  profileDir: (name: string) => `${fixture.root}/${name}`,
  profileConfigPath: (name: string) => `${fixture.root}/${name}/config.json`,
  profileLogPath: (name: string) => `${fixture.root}/${name}/daemon.log`,
  profilePidPath: (name: string) => `${fixture.root}/${name}/daemon.pid`,
}));
vi.mock("fs/promises", async (original) => {
  const fs = await original<typeof import("fs/promises")>();
  const safe = {
    ...fs,
    readFile: (path: string, ...args: any[]) => {
      if (path.endsWith("desktop_prefs.json"))
        return fs.readFile(`${fixture.root}/prefs.json`, "utf8");
      if (!String(path).startsWith(`${fixture.root}/`))
        throw new Error("Unexpected fixture read");
      return (fs.readFile as any)(path, ...args);
    },
    writeFile: (path: string, ...args: any[]) => {
      if (!String(path).startsWith(`${fixture.root}/`))
        throw new Error("Unexpected fixture write");
      return (fs.writeFile as any)(path, ...args);
    },
  };
  return { ...safe, default: safe };
});
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

beforeAll(async () => {
  Object.defineProperty(process, "contextIsolated", {
    configurable: true,
    value: true,
  });
  const fs = await vi.importActual<typeof import("fs/promises")>("fs/promises");
  const os = await import("os");
  fixture.root = await fs.mkdtemp(`${os.tmpdir()}/diagnostics-`);
  await fs.mkdir(`${fixture.root}/desktop-diagnostics.invalid`);
  await fs.mkdir(`${fixture.root}/resources/bin`, { recursive: true });
  await fs.writeFile(
    `${fixture.root}/resources/bin/multica`,
    "fixture executable boundary",
  );
  await fs.writeFile(
    `${fixture.root}/prefs.json`,
    JSON.stringify({ autoStart: false, autoStop: false }),
  );
  await fs.writeFile(
    `${fixture.root}/desktop-diagnostics.invalid/config.json`,
    JSON.stringify({
      server_url: "https://diagnostics.invalid",
      token: "fixtureConfigCanary42",
    }),
  );
  await fs.writeFile(
    `${fixture.root}/desktop-diagnostics.invalid/daemon.log`,
    '12:00:00.000 INF fixture-log-marker component=daemon\n12:00:01.000 INF Authorization: Basic fixtureHeaderCanary42\n12:00:02.000 INF {"token":"fixtureLogCanary42"}\n',
  );
  fixture.exec.mockImplementation(
    (
      bin: string,
      args: string[],
      _options: unknown,
      callback: (...args: any[]) => void,
    ) => {
      expect(bin.startsWith(`${fixture.root}/`)).toBe(true);
      expect(args).toEqual(["version", "--output", "json"]);
      callback(null, '{"version":"0.8.2-fixture-cli"}');
    },
  );
  fixture.install.mockImplementation(() => {
    throw new Error("Unexpected CLI installation");
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(() => {
      throw new Error("Unexpected network access");
    }),
  );
  const actualInterval = globalThis.setInterval;
  vi.spyOn(globalThis, "setInterval").mockImplementation(
    (...args: Parameters<typeof setInterval>) => {
      const timer = actualInterval(...args);
      fixture.intervals.push(timer);
      return timer;
    },
  );
  const { setupDaemonManager } = await import("../../../main/daemon-manager");
  setupDaemonManager(() => null);
  await fixture.handlers.get("daemon:is-cli-installed")!({});
  await fixture.handlers.get("daemon:set-target-api-url")!(
    {},
    "https://diagnostics.invalid",
  );
  await import("../../../preload/index");
});
afterEach(() => {
  cleanup();
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
});
afterAll(async () => {
  fixture.listeners.get("daemon:stop-log-stream")?.({ sender: fixture.sender });
  fixture.intervals.forEach(clearInterval);
  const fs = await vi.importActual<typeof import("fs/promises")>("fs/promises");
  await fs.rm(fixture.root, { recursive: true, force: true });
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(process, "contextIsolated");
});

function settings() {
  return render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <DaemonSettingsTab />
    </I18nProvider>,
  );
}

describe("Desktop diagnostics through production preload and registered IPC", () => {
  it("BASE control renders existing diagnostic fields", async () => {
    settings();
    expect(await screen.findByText("fixture-daemon")).toBeInTheDocument();
    expect(screen.getByText("desktop-diagnostics.invalid")).toBeInTheDocument();
    expect(fixture.install).not.toHaveBeenCalled();
  });
  it("BASE control opens the normal daemon log panel and reads the fixture file", async () => {
    render(
      <I18nProvider locale="en" resources={RESOURCES}>
        <DaemonPanel
          open
          onOpenChange={vi.fn()}
          status={{ state: "running" }}
          runtimeCount={0}
        />
      </I18nProvider>,
    );
    expect(await screen.findByText("fixture-log-marker")).toBeInTheDocument();
  });
  it("offers a local diagnostics export from the normal settings diagnostic section", async () => {
    const fs =
      await vi.importActual<typeof import("fs/promises")>("fs/promises");
    const configPath = `${fixture.root}/desktop-diagnostics.invalid/config.json`;
    const originalConfig = await fs.readFile(configPath, "utf8");
    const originalPrefs = await fs.readFile(
      `${fixture.root}/prefs.json`,
      "utf8",
    );
    fixture.save.mockResolvedValue({
      canceled: false,
      filePath: `${fixture.root}/diagnostics.zip`,
    });
    settings();
    await screen.findByText("fixture-daemon");
    expect(
      screen.getByText(/application.*CLI.*profile.*health.*log/i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Export diagnostics" }));
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith("Diagnostics saved"),
    );
    const archive = await fs.readFile(`${fixture.root}/diagnostics.zip`);
    const result = await readZip(new Uint8Array(archive), 64 * 1024);
    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error(result.error);
    expect(result.files.map((file) => file.path)).toEqual([
      "diagnostics.json",
      "daemon.log",
    ]);
    const metadata = JSON.parse(
      new TextDecoder().decode(result.files[0]!.data),
    );
    expect(metadata.appVersion).toBe("0.8.2-fixture");
    expect(metadata.cliVersion).toBe("0.8.2-fixture-cli");
    expect(metadata.daemon.lastObservedState).toBe("running");
    expect(new TextDecoder().decode(result.files[1]!.data)).toContain(
      "fixture-log-marker",
    );
    for (const secret of [
      "fixtureConfigCanary42",
      "fixtureHeaderCanary42",
      "fixtureLogCanary42",
    ])
      expect(archive.toString()).not.toContain(secret);
    expect(await fs.readFile(configPath, "utf8")).toBe(originalConfig);
    expect(await fs.readFile(`${fixture.root}/prefs.json`, "utf8")).toBe(
      originalPrefs,
    );
    expect(fixture.install).not.toHaveBeenCalled();
  });

  it("registers the daemon diagnostics in the actual Desktop Settings route", async () => {
    const { appRoutes } = await import("../routes");
    const settingsRoute = appRoutes[0]?.children
      ?.find((route) => route.path === ":workspaceSlug")
      ?.children?.find((route) => route.path === "settings");
    expect(settingsRoute?.element).toBeTruthy();
    registerAuthStore(
      createAuthStore({
        api: new ApiClient("https://diagnostics.invalid"),
        storage: { getItem: () => null, setItem: vi.fn(), removeItem: vi.fn() },
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    client.setQueryData(["workspaces", "list"], []);
    client.setQueryData(["workspaces", "", "members"], []);
    render(
      <QueryClientProvider client={client}>
        <I18nProvider locale="en" resources={RESOURCES}>
          <NavigationProvider
            value={{
              push: vi.fn(),
              replace: vi.fn(),
              back: vi.fn(),
              pathname: "/fixture/settings",
              searchParams: new URLSearchParams("tab=daemon"),
              hash: "",
              getShareableUrl: (path) => `https://diagnostics.invalid${path}`,
            }}
          >
            <WorkspaceSlugProvider slug="fixture">
              {settingsRoute?.element}
            </WorkspaceSlugProvider>
          </NavigationProvider>
        </I18nProvider>
      </QueryClientProvider>,
    );
    expect(await screen.findByText("fixture-daemon")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Export diagnostics" }),
    ).toBeInTheDocument();
    client.clear();
  });

  it("keeps one pending save dialog and silently returns from cancellation", async () => {
    let cancel!: (value: { canceled: boolean }) => void;
    fixture.save.mockClear().mockReturnValueOnce(
      new Promise((resolve) => {
        cancel = resolve;
      }),
    );
    settings();
    await screen.findByText("fixture-daemon");
    const button = screen.getByRole("button", { name: "Export diagnostics" });
    try {
      fireEvent.click(button);
      expect(button).toBeDisabled();
      expect(button).toHaveAttribute("aria-busy", "true");
      fireEvent.click(button);
      expect(fixture.save).toHaveBeenCalledTimes(1);
    } finally {
      cancel({ canceled: true });
    }
    await waitFor(() => expect(button).toBeEnabled());
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("reports save failures without raw error details and allows retry", async () => {
    fixture.save.mockRejectedValueOnce(new Error("token=dialogErrorCanary42"));
    settings();
    await screen.findByText("fixture-daemon");
    const button = screen.getByRole("button", { name: "Export diagnostics" });
    fireEvent.click(button);
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "Could not save diagnostics. Choose a writable location and try again.",
      ),
    );
    expect(button).toBeEnabled();
    fixture.save.mockResolvedValueOnce({ canceled: true });
    fireEvent.click(button);
    await waitFor(() => expect(button).toBeEnabled());
  });
});

describe("structured-log credentials through the settings export action", () => {
  it.each([
    ["token", "escapedCanary42"],
    ["password", "quotedCanary42"],
    ["Authorization", "Negotiate authCanary42"],
    ["Proxy-Authorization", "Digest proxyCanary42"],
  ])(
    "redacts escaped JSON %s inside structured logger errors",
    async (key, secret) => {
      const fs =
        await vi.importActual<typeof import("fs/promises")>("fs/promises");
      const actualLine =
        "12:00:00.000 ERR provider request failed error=" +
        JSON.stringify("HTTP 401 " + JSON.stringify({ [key]: secret })) +
        " component=daemon" +
        String.fromCharCode(10);
      await fs.writeFile(
        fixture.root + "/desktop-diagnostics.invalid/daemon.log",
        actualLine,
      );
      fixture.save.mockResolvedValue({
        canceled: false,
        filePath: fixture.root + "/review-security.zip",
      });
      settings();
      await screen.findByText("fixture-daemon");
      fireEvent.click(
        screen.getByRole("button", { name: "Export diagnostics" }),
      );
      await waitFor(() =>
        expect(toast.success).toHaveBeenCalledWith("Diagnostics saved"),
      );
      const archive = await fs.readFile(fixture.root + "/review-security.zip");
      const extracted = await readZip(new Uint8Array(archive), 64 * 1024);
      expect(extracted.ok).toBe(true);
      if (!extracted.ok) throw new Error(extracted.error);
      const log = new TextDecoder().decode(
        extracted.files.find((file) => file.path === "daemon.log")!.data,
      );
      expect(log).toContain("provider request failed");
      expect(log).not.toContain(secret);
    },
  );
});

describe("bounded credential context through the normal diagnostics action", () => {
  it.each([
    ["complete short quoted password", false, false, true, false],
    ["truncated quoted password", true, false, true, false],
    ["complete malformed encoded token", false, true, true, false],
    ["truncated malformed encoded token", true, true, true, false],
    ["unclosed short password", false, false, false, false],
    ["unclosed long password", true, false, false, false],
    ["unclosed short encoded token", false, true, false, false],
    ["unclosed long encoded token", true, true, false, false],
    ["over-budget password", true, false, true, true],
    ["over-budget encoded token", true, true, true, true],
    ["over-budget unclosed password", true, false, false, true],
    ["over-budget unclosed encoded token", true, true, false, true],
  ])(
    "redacts %s via normal Settings and extracted ZIP",
    async (name, truncated, encoded, closed, overBudget) => {
      const fs =
        await vi.importActual<typeof import("fs/promises")>("fs/promises");
      const canary = "cutCanary42";
      const padding = overBudget
        ? "padding".repeat(40000)
        : truncated
          ? "padding".repeat(6000)
          : "short";
      const prefix = encoded
        ? "12:00:00.000 ERR provider request failed error=" +
          String.raw`"HTTP 401 {\"token\":\"` +
          padding
        : '12:00:00.000 ERR provider request failed password="' + padding;
      const suffix = encoded ? String.raw`last\"}"` : 'last"';
      const input =
        prefix +
        "\n12:00:01.000 INF " +
        canary +
        "\n" +
        (closed ? suffix : "") +
        "\n12:00:02.000 INF safe-final-marker component=daemon\n";
      await fs.writeFile(
        fixture.root + "/desktop-diagnostics.invalid/daemon.log",
        input,
      );
      fixture.save.mockResolvedValue({
        canceled: false,
        filePath: fixture.root + "/boundary-review.zip",
      });
      settings();
      await screen.findByText("fixture-daemon");
      fireEvent.click(
        screen.getByRole("button", { name: "Export diagnostics" }),
      );
      await waitFor(() =>
        expect(toast.success).toHaveBeenCalledWith("Diagnostics saved"),
      );
      const archive = await fs.readFile(fixture.root + "/boundary-review.zip");
      const result = await readZip(new Uint8Array(archive), 64 * 1024);
      if (!result.ok) throw new Error(result.error);
      const log = new TextDecoder().decode(
        result.files.find((f) => f.path === "daemon.log")!.data,
      );
      const metadata = JSON.parse(
        new TextDecoder().decode(
          result.files.find((f) => f.path === "diagnostics.json")!.data,
        ),
      );
      const record = {
        name,
        inputBytes: Buffer.byteLength(input),
        inputPrefix: input.slice(0, 150),
        inputSuffix: input.slice(-250),
        extracted: log,
        metadata,
        archiveBytes: archive.length,
      };
      const artifactRoot = process.env.BOYF788_BOUNDARY_ARTIFACTS;
      if (artifactRoot) {
        await fs.mkdir(artifactRoot, { recursive: true });
        const basename = String(name).replaceAll(" ", "-");
        await fs.writeFile(artifactRoot + "/" + basename + "-input.log", input);
        await fs.writeFile(artifactRoot + "/" + basename + ".zip", archive);
        await fs.writeFile(
          artifactRoot + "/" + basename + "-extracted.log",
          log,
        );
        console.log("BOUNDARY_REVIEW", JSON.stringify(record));
        await fs.appendFile(
          process.env.BOYF788_BOUNDARY_CAPTURE!,
          JSON.stringify(record) + "\n",
        );
      }
      expect(archive.toString()).not.toContain(canary);
      expect(archive.byteLength).toBeLessThanOrEqual(64 * 1024);
      expect(Buffer.byteLength(log)).toBeLessThanOrEqual(32 * 1024);
      if (overBudget) {
        expect(log).toBe("");
        expect(metadata.log.omissionReason).toBe("source_limit");
        expect(metadata.log.status).toBe("unavailable");
      } else {
        expect(log).toContain("provider request failed");
        if (closed) {
          expect(log).toContain("safe-final-marker component=daemon");
          expect(metadata.log.omissionReason).toBeNull();
        } else {
          expect(log).not.toContain("safe-final-marker");
          expect(metadata.log.omissionReason).toBe("incomplete_value");
          expect(metadata.log.truncated).toBe(true);
        }
      }
      expect(metadata.daemon.lastObservedState).toBe("running");
    },
  );
});
