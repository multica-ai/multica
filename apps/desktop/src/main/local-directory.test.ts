// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IpcMainInvokeEvent } from "electron";
import { constants } from "fs";
import { setupLocalDirectory } from "./local-directory";

const mocks = vi.hoisted(() => ({
  handle: vi.fn(),
  showOpenDialog: vi.fn(),
  fromWebContents: vi.fn(),
  stat: vi.fn(),
  access: vi.fn(),
}));
vi.mock("electron", () => ({
  ipcMain: { handle: mocks.handle },
  dialog: { showOpenDialog: mocks.showOpenDialog },
  BrowserWindow: { fromWebContents: mocks.fromWebContents },
}));
vi.mock("fs/promises", () => ({ stat: mocks.stat, access: mocks.access }));

const event = { sender: {} } as IpcMainInvokeEvent;
function invoke(channel: string, path?: string) {
  const handler = mocks.handle.mock.calls.find(([name]) => name === channel)?.[1];
  if (!handler) throw new Error(`Missing handler: ${channel}`);
  return handler(event, path);
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.fromWebContents.mockReturnValue({});
  setupLocalDirectory(() => null);
});

describe("local directory picker IPC", () => {
  it("returns the selected folder and requests one directory with a starting path", async () => {
    mocks.showOpenDialog.mockResolvedValue({ canceled: false, filePaths: ["/projects/demo"] });
    await expect(invoke("local-directory:pick", "/projects")).resolves.toEqual({
      ok: true, path: "/projects/demo", basename: "demo",
    });
    expect(mocks.showOpenDialog).toHaveBeenCalledWith({}, {
      properties: ["openDirectory", "createDirectory"], defaultPath: "/projects",
    });
  });

  it.each([
    { canceled: true, filePaths: ["/projects/demo"] },
    { canceled: false, filePaths: [] },
  ])("preserves cancellation for %j", async (result) => {
    mocks.showOpenDialog.mockResolvedValue(result);
    await expect(invoke("local-directory:pick")).resolves.toEqual({ ok: false, reason: "cancelled" });
  });

  it("reports native dialog errors", async () => {
    mocks.showOpenDialog.mockRejectedValue(new Error("Dialog unavailable"));
    await expect(invoke("local-directory:pick")).resolves.toEqual({
      ok: false, reason: "error", error: "Dialog unavailable",
    });
  });
});

describe("local directory validation IPC", () => {
  it("accepts a readable and writable directory", async () => {
    mocks.stat.mockResolvedValueOnce({ isDirectory: () => true }).mockRejectedValue(new Error("No git entry"));
    mocks.access.mockResolvedValue(undefined);
    await expect(invoke("local-directory:validate", "/projects/demo")).resolves.toEqual({
      ok: true, is_git_repo: false,
    });
    expect(mocks.access).toHaveBeenCalledWith("/projects/demo", constants.R_OK);
    expect(mocks.access).toHaveBeenCalledWith("/projects/demo", constants.W_OK);
  });

  it("rejects files even if returned by a broken native picker", async () => {
    mocks.stat.mockResolvedValue({ isDirectory: () => false });
    await expect(invoke("local-directory:validate", "/projects/file.txt")).resolves.toEqual({
      ok: false, reason: "not_a_directory",
    });
  });

  it.each([
    [constants.R_OK, "not_readable"],
    [constants.W_OK, "not_writable"],
  ])("preserves permission failure %s", async (mode, reason) => {
    mocks.stat.mockResolvedValue({ isDirectory: () => true });
    mocks.access.mockImplementation(async (_path, requestedMode) => {
      if (requestedMode === mode) throw new Error("Permission denied");
    });
    await expect(invoke("local-directory:validate", "/projects/demo")).resolves.toEqual({ ok: false, reason });
  });
});
