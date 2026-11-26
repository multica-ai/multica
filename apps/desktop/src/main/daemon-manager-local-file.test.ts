// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  handlers: new Map<string, (...args: unknown[]) => unknown>(),
  health: vi.fn(),
  read: vi.fn(),
  write: vi.fn(),
  mkdir: vi.fn(),
  exec: vi.fn(),
  install: vi.fn(),
  token: "synthetic-token" as unknown,
}));

vi.mock("electron", () => ({
  app: {
    getAppPath: () => "/synthetic/desktop",
    getPreferredSystemLanguages: () => ["en"],
    on: vi.fn(),
  },
  dialog: { showMessageBox: vi.fn() },
  BrowserWindow: { getAllWindows: () => [] },
  ipcMain: {
    handle: (name: string, handler: (...args: unknown[]) => unknown) => mocks.handlers.set(name, handler),
    on: vi.fn(),
  },
}));
vi.mock("child_process", () => ({ execFile: mocks.exec }));
vi.mock("fs", async (original) => ({
  ...await original<typeof import("fs")>(),
  existsSync: vi.fn(() => true),
  watchFile: vi.fn(),
  unwatchFile: vi.fn(),
}));
vi.mock("fs/promises", async (original) => ({
  ...await original<typeof import("fs/promises")>(),
  readFile: mocks.read,
  writeFile: mocks.write,
  mkdir: mocks.mkdir,
}));
vi.mock("./cli-bootstrap", () => ({
  ensureManagedCli: mocks.install,
  managedCliPath: () => "/synthetic/managed/multica",
}));
vi.mock("./daemon-port", () => ({
  probeProfileHealth: mocks.health,
  readDaemonPortRecord: vi.fn(async () => null),
}));

const apiUrl = "https://server.example.test";
const profile = "desktop-server.example.test";

async function setup() {
  const manager = await import("./daemon-manager");
  manager.setupDaemonManager(() => null);
  await mocks.handlers.get("daemon:set-target-api-url")!({}, apiUrl);
  await mocks.handlers.get("daemon:is-cli-installed")!({});
  await mocks.handlers.get("daemon:get-status")!({});
  mocks.read.mockClear();
  mocks.write.mockClear();
  mocks.mkdir.mockClear();
  mocks.exec.mockClear();
  mocks.health.mockClear();
  return manager;
}

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  vi.useFakeTimers();
  mocks.handlers.clear();
  mocks.token = "synthetic-token";
  mocks.read.mockImplementation(async (path: string) => path.endsWith("desktop_prefs.json")
    ? JSON.stringify({ autoStart: false, autoStop: false })
    : JSON.stringify({ server_url: apiUrl, token: mocks.token }));
  mocks.health.mockResolvedValue({
    status: "running", daemon_id: "local-daemon",
    os: process.platform === "win32" ? "windows" : process.platform,
    server_url: apiUrl, profile,
  });
  mocks.exec.mockImplementation((_bin: string, args: string[], _options: unknown, callback: (error: null, output?: string) => void) => {
    callback(null, args[0] === "version" ? '{"version":"test"}' : "");
  });
  vi.spyOn(console, "log").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("local file access uses only a resolved Desktop profile", () => {
  it("does nothing before the Desktop profile exists", async () => {
    const { getLocalFileAccessContext } = await import("./daemon-manager");
    await expect(getLocalFileAccessContext()).resolves.toBeNull();
    expect(mocks.read).not.toHaveBeenCalled();
    expect(mocks.health).not.toHaveBeenCalled();
    expect(mocks.exec).not.toHaveBeenCalled();
    expect(mocks.write).not.toHaveBeenCalled();
  });

  it("reads the active profile and health without any lifecycle or config writes", async () => {
    const { getLocalFileAccessContext } = await setup();
    await expect(getLocalFileAccessContext()).resolves.toEqual({ apiUrl, token: "synthetic-token", daemonId: "local-daemon" });
    expect(mocks.read).toHaveBeenCalledWith(expect.stringContaining("/profiles/" + profile + "/config.json"), "utf-8");
    expect(mocks.health).toHaveBeenCalledWith(profile, expect.any(Function));
    expect(mocks.exec).not.toHaveBeenCalled();
    expect(mocks.write).not.toHaveBeenCalled();
    expect(mocks.mkdir).not.toHaveBeenCalled();
  });

  it.each([
    null,
    { status: "stopped" },
    { status: "running", daemon_id: "local-daemon", os: "other-os", server_url: apiUrl },
    { status: "running", daemon_id: "local-daemon", server_url: apiUrl },
    { status: "running", daemon_id: "local-daemon", os: process.platform, server_url: "https://other.example.test" },
  ])("rejects unresolved or mismatched daemon identity %j", async (health) => {
    const { getLocalFileAccessContext } = await setup();
    mocks.health.mockResolvedValue(health);
    await expect(getLocalFileAccessContext()).resolves.toBeNull();
    expect(mocks.exec).not.toHaveBeenCalled();
    expect(mocks.write).not.toHaveBeenCalled();
  });

  it("rejects missing credentials without starting authentication", async () => {
    const { getLocalFileAccessContext } = await setup();
    mocks.token = null;
    await expect(getLocalFileAccessContext()).resolves.toBeNull();
    expect(mocks.exec).not.toHaveBeenCalled();
    expect(mocks.write).not.toHaveBeenCalled();
  });
});
