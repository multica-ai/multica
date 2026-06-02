import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { installRendererRecoveryHandlers } from "./renderer-recovery";

type Handler = (...args: unknown[]) => void;

function makeWindow() {
  const windowHandlers = new Map<string, Handler>();
  const webContentsHandlers = new Map<string, Handler>();
  const reload = vi.fn();
  return {
    window: {
      on: vi.fn((event: string, handler: Handler) => windowHandlers.set(event, handler)),
      isDestroyed: vi.fn(() => false),
      webContents: {
        on: vi.fn((event: string, handler: Handler) => webContentsHandlers.set(event, handler)),
        reload,
      },
    },
    windowHandlers,
    webContentsHandlers,
    reload,
  };
}

describe("installRendererRecoveryHandlers", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.useRealTimers());

  it("registers production reload prompts for renderer death and preload failure without auto reloading", async () => {
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "reload" as const);

    installRendererRecoveryHandlers(fixture.window, { isDev: false, showReloadPrompt });

    expect(fixture.webContentsHandlers.has("render-process-gone")).toBe(true);
    expect(fixture.webContentsHandlers.has("preload-error")).toBe(true);
    expect(fixture.windowHandlers.has("unresponsive")).toBe(true);
    expect(fixture.windowHandlers.has("responsive")).toBe(true);

    fixture.webContentsHandlers.get("render-process-gone")?.({}, { reason: "crashed" });
    fixture.webContentsHandlers.get("preload-error")?.({}, "/preload.js", new Error("boom"));

    expect(fixture.reload).not.toHaveBeenCalled();
    await Promise.resolve();

    expect(showReloadPrompt).toHaveBeenCalledTimes(2);
    expect(fixture.reload).toHaveBeenCalledTimes(2);
  });

  it("does not prompt when the renderer exits cleanly", async () => {
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "reload" as const);

    installRendererRecoveryHandlers(fixture.window, { isDev: false, showReloadPrompt });

    fixture.webContentsHandlers.get("render-process-gone")?.({}, { reason: "clean-exit" });
    await Promise.resolve();

    expect(showReloadPrompt).not.toHaveBeenCalled();
    expect(fixture.reload).not.toHaveBeenCalled();
  });

  it("cancels an unresponsive prompt when the window becomes responsive again", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "reload" as const);

    installRendererRecoveryHandlers(fixture.window, {
      isDev: false,
      showReloadPrompt,
      unresponsivePromptDelayMs: 100,
    });

    fixture.windowHandlers.get("unresponsive")?.();
    fixture.windowHandlers.get("responsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(showReloadPrompt).not.toHaveBeenCalled();
    expect(fixture.reload).not.toHaveBeenCalled();
  });

  it("prompts for sustained unresponsive windows and only reloads after user confirmation", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "dismiss" as const);

    installRendererRecoveryHandlers(fixture.window, {
      isDev: false,
      showReloadPrompt,
      unresponsivePromptDelayMs: 100,
    });

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(showReloadPrompt).toHaveBeenCalledWith({ kind: "unresponsive", context: {} });
    expect(fixture.reload).not.toHaveBeenCalled();
  });

  it("keeps dev diagnostics non-prompting", async () => {
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "reload" as const);

    installRendererRecoveryHandlers(fixture.window, { isDev: true, showReloadPrompt, log: vi.fn() });

    fixture.webContentsHandlers.get("render-process-gone")?.({}, { reason: "crashed" });
    await Promise.resolve();

    expect(showReloadPrompt).not.toHaveBeenCalled();
    expect(fixture.reload).not.toHaveBeenCalled();
  });
});
