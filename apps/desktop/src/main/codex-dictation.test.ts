// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow, IpcMainInvokeEvent } from "electron";
import { CODEX_DICTATION_CHANNEL } from "../shared/dictation";

const mocks = vi.hoisted(() => ({
  handle: vi.fn(),
  fromWebContents: vi.fn(),
  open: vi.fn(),
  execFile: vi.fn(),
  getAppPath: vi.fn(),
}));
vi.mock("electron", () => ({
  app: { getAppPath: mocks.getAppPath },
  BrowserWindow: { fromWebContents: mocks.fromWebContents },
  ipcMain: { handle: mocks.handle },
}));
vi.mock("node:fs/promises", () => ({ open: mocks.open }));
vi.mock("node:child_process", () => ({ execFile: mocks.execFile }));

import {
  hasCodexDictationBinding,
  registerCodexDictationWindow,
  setupCodexDictation,
} from "./codex-dictation";

const binding = JSON.stringify([
  { command: "globalDictationToggle", key: "Ctrl+Alt+Shift+D" },
]);

describe("dictation keybindings", () => {
  it("accepts only the explicitly configured global toggle chord", () => {
    expect(hasCodexDictationBinding(binding)).toBe(true);
    expect(hasCodexDictationBinding(JSON.stringify([
      { command: "globalDictationToggle", key: "Shift+Control+Alt+d" },
    ]))).toBe(true);
  });

  it.each([
    "not json", "{}", "[]", "null",
    JSON.stringify([{ command: "startDictation", key: "Ctrl+Alt+Shift+D" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Shift+D" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Alt+Shift+Q" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Alt+Shift+D", when: "editor" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Alt+Shift+D" }, { command: "globalDictationToggle", key: "" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Alt+Shift+D" }, { command: "anotherCommand", key: "Control+Alt+Shift+D" }]),
    JSON.stringify([{ command: "globalDictationToggle", key: "Ctrl+Alt+Shift+D; Remove-Item x" }]),
    " ".repeat(65537),
  ])("rejects missing, ambiguous and unsafe bindings", (raw) => {
    expect(hasCodexDictationBinding(raw)).toBe(false);
  });
});

describe("dictation IPC", () => {
  const platformDescriptor = Object.getOwnPropertyDescriptor(process, "platform")!;
  let focused: boolean;
  let raw: string;
  let fakeWindow: BrowserWindow;
  let event: IpcMainInvokeEvent;
  let invoke: (event: IpcMainInvokeEvent, ...args: unknown[]) => Promise<unknown>;
  let windowEvents: ReturnType<typeof vi.fn>;
  const close = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(process, "platform", { value: "win32", configurable: true });
    vi.stubEnv("CODEX_HOME", "/CodexHome");
    mocks.getAppPath.mockReturnValue("/Multica/resources/app.asar");
    focused = true;
    raw = binding;
    const handle = Buffer.alloc(8);
    handle.writeBigUInt64LE(42n);
    const contents = {
      on: vi.fn(),
      isDestroyed: () => false,
      mainFrame: { url: "file:///C:/Multica/out/renderer/index.html" },
      executeJavaScript: vi.fn().mockResolvedValue(true),
      executeJavaScriptInIsolatedWorld: vi.fn().mockResolvedValue(true),
    };
    windowEvents = contents.on;
    fakeWindow = {
      isDestroyed: () => false,
      isFocused: () => focused,
      getNativeWindowHandle: () => handle,
      webContents: contents,
    } as unknown as BrowserWindow;
    const frame = contents.mainFrame;
    event = {
      senderFrame: frame,
      sender: contents,
    } as unknown as IpcMainInvokeEvent;
    mocks.fromWebContents.mockReturnValue(fakeWindow);
    registerCodexDictationWindow(fakeWindow, frame.url);
    mocks.open.mockResolvedValue({
      stat: async () => ({ isFile: () => true, size: Buffer.byteLength(raw) }),
      read: async (buffer: Buffer) => ({ bytesRead: buffer.write(raw) }),
      close,
    });
    mocks.execFile.mockImplementation((_path, _args, _options, callback) => {
      callback(null, "sent\n", "");
    });
    setupCodexDictation();
    expect(mocks.handle).toHaveBeenCalledWith(CODEX_DICTATION_CHANNEL, expect.any(Function));
    invoke = mocks.handle.mock.calls[0]![1];
  });

  afterEach(() => {
    Object.defineProperty(process, "platform", platformDescriptor);
    vi.unstubAllEnvs();
    vi.useRealTimers();
  });

  it("reads only keybindings and dispatches a bounded hidden fixed-key helper", async () => {
    expect(await invoke(event)).toEqual({ ok: true, shortcut: "Ctrl+Alt+Shift+D" });
    expect(mocks.open).toHaveBeenCalledOnce();
    expect(mocks.open.mock.calls[0]![0]).toMatch(/keybindings\.json$/);
    expect(mocks.open.mock.calls[0]![1]).toBe("r");
    expect(close).toHaveBeenCalledOnce();
    const [path, args, options] = mocks.execFile.mock.calls[0]!;
    expect(path).toMatch(/app\.asar\.unpacked[\\/]resources[\\/]bin[\\/]multica\.exe$/);
    expect(args).toEqual(["--desktop-dictation-v1", "toggle", "42"]);
    expect(options).toMatchObject({ windowsHide: true, shell: false, timeout: 5000, maxBuffer: 4096 });
  });

  it("rejects renderer-supplied payloads, even a plausible chord", async () => {
    expect(await invoke(event, "Ctrl+Alt+Shift+D")).toMatchObject({ ok: false });
    expect(mocks.open).not.toHaveBeenCalled();
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it.each(["unregistered", "background", "subframe", "foreign", "missing-frame"])(
    "rejects a %s sender before filesystem or OS access", async (kind) => {
      if (kind === "unregistered") mocks.fromWebContents.mockReturnValue({ isDestroyed: () => false, isFocused: () => true });
      if (kind === "background") focused = false;
      if (kind === "subframe") event = { ...event, senderFrame: { url: event.senderFrame!.url } } as IpcMainInvokeEvent;
      if (kind === "foreign") Object.defineProperty(event.senderFrame, "url", { value: "https://evil.example/" });
      if (kind === "missing-frame") event = { ...event, senderFrame: null } as IpcMainInvokeEvent;
      expect(await invoke(event)).toEqual({ ok: false, reason: "not_focused" });
      expect(mocks.open).not.toHaveBeenCalled();
      expect(mocks.execFile).not.toHaveBeenCalled();
    },
  );

  it("ignores spoofed main-world activation and requires the isolated mic grant", async () => {
    vi.mocked(event.sender.executeJavaScriptInIsolatedWorld).mockResolvedValue(false);
    expect(await invoke(event)).toEqual({ ok: false, reason: "unavailable" });
    expect(event.sender.executeJavaScript).not.toHaveBeenCalled();
    expect(mocks.open).not.toHaveBeenCalled();
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it("times out a stalled renderer instead of locking dictation forever", async () => {
    vi.useFakeTimers();
    vi.mocked(event.sender.executeJavaScriptInIsolatedWorld).mockReturnValue(new Promise(() => {}));
    const result = invoke(event);
    await vi.advanceTimersByTimeAsync(1000);
    expect(await result).toEqual({ ok: false, reason: "unavailable" });
    vi.mocked(event.sender.executeJavaScriptInIsolatedWorld).mockResolvedValue(true);
    expect(await invoke(event)).toMatchObject({ ok: true });
  });

  it("does not emit any chord if configuration is absent or too large", async () => {
    mocks.open.mockRejectedValueOnce(new Error("ENOENT"));
    expect(await invoke(event)).toEqual({ ok: false, reason: "not_configured" });
    raw = " ".repeat(65537);
    expect(await invoke(event)).toEqual({ ok: false, reason: "not_configured" });
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it("rechecks focus after asynchronous configuration access", async () => {
    mocks.open.mockImplementationOnce(async () => {
      focused = false;
      return {
        stat: async () => ({ isFile: () => true, size: binding.length }),
        read: async (buffer: Buffer) => ({ bytesRead: buffer.write(binding) }),
        close,
      };
    });
    expect(await invoke(event)).toEqual({ ok: false, reason: "not_focused" });
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it("allows only one in-flight toggle across all windows", async () => {
    const secondWindow = {
      ...fakeWindow,
      webContents: { ...event.sender, on: vi.fn(), executeJavaScriptInIsolatedWorld: vi.fn().mockResolvedValue(true) },
    } as unknown as BrowserWindow;
    const secondEvent = { ...event, sender: secondWindow.webContents } as IpcMainInvokeEvent;
    registerCodexDictationWindow(secondWindow, event.senderFrame!.url);
    mocks.fromWebContents.mockImplementation((sender) =>
      sender === secondEvent.sender ? secondWindow : fakeWindow,
    );
    let complete!: (error: null, stdout: string) => void;
    mocks.execFile.mockImplementation((_path, _args, _options, callback) => { complete = callback; });
    const first = invoke(event);
    await vi.waitFor(() => expect(mocks.execFile).toHaveBeenCalledOnce());
    expect(await invoke(secondEvent)).toEqual({ ok: false, reason: "busy" });
    expect(secondEvent.sender.executeJavaScriptInIsolatedWorld).not.toHaveBeenCalled();
    complete(null, "sent");
    expect(await first).toMatchObject({ ok: true });
  });

  it("blocks the unconsumed fixed chord before it can edit a draft, including AltGr layouts", () => {
    const registration = windowEvents.mock.calls.find(
      ([name]) => name === "before-input-event",
    );
    expect(registration).toBeDefined();
    const listener = registration![1] as (event: { preventDefault: () => void }, input: object) => void;
    for (const key of ["D", "Đ"]) {
      const preventDefault = vi.fn();
      listener({ preventDefault }, { type: "keyDown", key, code: "KeyD", control: true, alt: true, shift: true, meta: false });
      expect(preventDefault).toHaveBeenCalledOnce();
    }
    for (const change of [{ control: false }, { alt: false }, { shift: false }, { meta: true }, { code: "KeyE", key: "E" }]) {
      const preventDefault = vi.fn();
      listener({ preventDefault }, { type: "keyDown", key: "D", code: "KeyD", control: true, alt: true, shift: true, meta: false, ...change });
      expect(preventDefault).not.toHaveBeenCalled();
    }
  });

  it.each(["app_not_running", "not_focused", "busy", "cleanup_failed"])("preserves native failure %s", async (reason) => {
    mocks.execFile.mockImplementation((_path, _args, _options, callback) => callback(null, reason, ""));
    expect(await invoke(event)).toEqual({ ok: false, reason });
  });

  it("never returns subprocess details or retries after a helper failure", async () => {
    mocks.execFile.mockImplementation((_path, _args, _options, callback) => callback(new Error("private path"), "private output", "private stderr"));
    expect(await invoke(event)).toEqual({ ok: false, reason: "unavailable" });
    expect(mocks.execFile).toHaveBeenCalledOnce();
  });

  it("fails closed when the bundled CLI is missing or too old", async () => {
    mocks.execFile.mockImplementationOnce((_path, _args, _options, callback) => callback(new Error("ENOENT"), "", ""));
    expect(await invoke(event)).toEqual({ ok: false, reason: "unavailable" });
    mocks.execFile.mockImplementationOnce((_path, _args, _options, callback) => callback(null, "unknown command", ""));
    expect(await invoke(event)).toEqual({ ok: false, reason: "unavailable" });
    expect(mocks.execFile).toHaveBeenCalledTimes(2);
  });

  it.each([Buffer.alloc(8), Buffer.alloc(3)])("rejects a zero or malformed native window handle", async (handle) => {
    vi.spyOn(fakeWindow, "getNativeWindowHandle").mockReturnValue(handle);
    expect(await invoke(event)).toEqual({ ok: false, reason: "not_focused" });
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it("does nothing on unsupported platforms", async () => {
    Object.defineProperty(process, "platform", { value: "linux", configurable: true });
    expect(await invoke(event)).toEqual({ ok: false, reason: "unavailable" });
    expect(mocks.open).not.toHaveBeenCalled();
    expect(mocks.execFile).not.toHaveBeenCalled();
  });
});
