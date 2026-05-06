// task-change-api.test.ts
//
// Surface-area tests for the `taskChangeAPI` preload bridge. The point of
// this contract is to make it impossible for the renderer to ask main for
// arbitrary filesystem reads/writes — only the four narrow methods listed
// in the spec are exposed. These tests load `preload/index.ts` with
// `electron` mocked, then inspect the object passed to
// `contextBridge.exposeInMainWorld("taskChangeAPI", ...)`.

import { vi, describe, it, expect, beforeEach } from "vitest";

const mockExpose = vi.hoisted(() => vi.fn());
const mockInvoke = vi.hoisted(() => vi.fn());
const mockOn = vi.hoisted(() => vi.fn());
const mockRemoveListener = vi.hoisted(() => vi.fn());
const mockSend = vi.hoisted(() => vi.fn());
const mockSendSync = vi.hoisted(() => vi.fn(() => undefined));

vi.mock("electron", () => ({
  contextBridge: { exposeInMainWorld: mockExpose },
  ipcRenderer: {
    invoke: mockInvoke,
    on: mockOn,
    removeListener: mockRemoveListener,
    send: mockSend,
    sendSync: mockSendSync,
  },
}));

vi.mock("@electron-toolkit/preload", () => ({ electronAPI: {} }));

// `process.contextIsolated` is undefined in jsdom; force the bridge path.
Object.defineProperty(process, "contextIsolated", {
  configurable: true,
  value: true,
});

function getTaskChangeSurface(): Record<string, unknown> {
  const call = mockExpose.mock.calls.find((c) => c[0] === "taskChangeAPI");
  expect(call, "expected taskChangeAPI to be exposed").toBeTruthy();
  return call![1] as Record<string, unknown>;
}

describe("taskChangeAPI surface", () => {
  beforeEach(() => {
    vi.resetModules();
    mockExpose.mockClear();
    mockInvoke.mockReset();
    mockOn.mockClear();
    mockRemoveListener.mockClear();
    mockSend.mockClear();
    mockSendSync.mockClear();
  });

  it("exposes exactly the four documented methods", async () => {
    await import("./index");
    const surface = getTaskChangeSurface();

    expect(Object.keys(surface).sort()).toEqual([
      "applyTaskDiff",
      "openPath",
      "pickCheckoutDirectory",
      "previewApplyTaskDiff",
    ]);
    expect(typeof surface.applyTaskDiff).toBe("function");
    expect(typeof surface.openPath).toBe("function");
    expect(typeof surface.pickCheckoutDirectory).toBe("function");
    expect(typeof surface.previewApplyTaskDiff).toBe("function");
  });

  it("does not expose any arbitrary fs / process methods", async () => {
    await import("./index");
    const surface = getTaskChangeSurface();

    for (const name of Object.keys(surface)) {
      expect(name).not.toMatch(
        /\b(readFile|writeFile|readDir|listDir|readPath|writePath|exec|spawn|fork|require|eval)\b/i,
      );
    }
  });

  it("each method routes through ipcRenderer.invoke with the matching channel", async () => {
    await import("./index");
    const surface = getTaskChangeSurface() as {
      pickCheckoutDirectory: () => Promise<unknown>;
      previewApplyTaskDiff: (input: unknown) => Promise<unknown>;
      applyTaskDiff: (input: unknown) => Promise<unknown>;
      openPath: (target: string) => Promise<unknown>;
    };

    mockInvoke.mockResolvedValue("noop");

    await surface.pickCheckoutDirectory();
    expect(mockInvoke).toHaveBeenLastCalledWith(
      "taskChange:pickCheckoutDirectory",
    );

    const fakeInput = { taskWorktreePath: "/x" };
    await surface.previewApplyTaskDiff(fakeInput);
    expect(mockInvoke).toHaveBeenLastCalledWith(
      "taskChange:previewApplyTaskDiff",
      fakeInput,
    );

    await surface.applyTaskDiff(fakeInput);
    expect(mockInvoke).toHaveBeenLastCalledWith(
      "taskChange:applyTaskDiff",
      fakeInput,
    );

    await surface.openPath("/some/abs/path");
    expect(mockInvoke).toHaveBeenLastCalledWith(
      "taskChange:openPath",
      "/some/abs/path",
    );
  });
});
