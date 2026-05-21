import { describe, expect, it, vi } from "vitest";
import { installRendererFreezeWatchdog } from "./renderer-freeze-watchdog";

type Handler = (...args: unknown[]) => void;

function makeWindow() {
  const windowHandlers = new Map<string, Handler[]>();
  const webHandlers = new Map<string, Handler[]>();
  const reloadIgnoringCache = vi.fn();
  let destroyed = false;
  let webContentsDestroyed = false;

  const addHandler = (map: Map<string, Handler[]>, event: string, handler: Handler) => {
    const handlers = map.get(event) ?? [];
    handlers.push(handler);
    map.set(event, handlers);
  };

  return {
    win: {
      on: vi.fn((event: string, handler: Handler) => addHandler(windowHandlers, event, handler)),
      isDestroyed: vi.fn(() => destroyed),
      webContents: {
        on: vi.fn((event: string, handler: Handler) => addHandler(webHandlers, event, handler)),
        isDestroyed: vi.fn(() => webContentsDestroyed),
        reloadIgnoringCache,
      },
    },
    emitWindow: (event: string) => {
      for (const handler of windowHandlers.get(event) ?? []) handler();
    },
    emitWebContents: (event: string, ...args: unknown[]) => {
      for (const handler of webHandlers.get(event) ?? []) handler(...args);
    },
    reloadIgnoringCache,
    destroyWindow: () => {
      destroyed = true;
    },
    destroyWebContents: () => {
      webContentsDestroyed = true;
    },
  };
}

const logger = {
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
};

describe("installRendererFreezeWatchdog", () => {
  it("reloads the renderer when a Windows freeze stays unresponsive past the timeout", () => {
    vi.useFakeTimers();
    const fx = makeWindow();

    installRendererFreezeWatchdog(fx.win as never, {
      timeoutMs: 1_000,
      logger,
    });

    fx.emitWindow("unresponsive");
    vi.advanceTimersByTime(999);
    expect(fx.reloadIgnoringCache).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1);
    expect(fx.reloadIgnoringCache).toHaveBeenCalledTimes(1);

    vi.useRealTimers();
  });

  it("cancels the reload when the renderer becomes responsive again", () => {
    vi.useFakeTimers();
    const fx = makeWindow();

    installRendererFreezeWatchdog(fx.win as never, {
      timeoutMs: 1_000,
      logger,
    });

    fx.emitWindow("unresponsive");
    fx.emitWindow("responsive");
    vi.advanceTimersByTime(1_000);

    expect(fx.reloadIgnoringCache).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  it("does not reload after the window is destroyed", () => {
    vi.useFakeTimers();
    const fx = makeWindow();

    installRendererFreezeWatchdog(fx.win as never, {
      timeoutMs: 1_000,
      logger,
    });

    fx.emitWindow("unresponsive");
    fx.destroyWindow();
    vi.advanceTimersByTime(1_000);

    expect(fx.reloadIgnoringCache).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  it("logs renderer process exits for post-freeze diagnostics", () => {
    const fx = makeWindow();

    installRendererFreezeWatchdog(fx.win as never, { logger });
    fx.emitWebContents("render-process-gone", {}, { reason: "crashed" });

    expect(logger.error).toHaveBeenCalledWith(
      "[renderer] render process gone",
      { reason: "crashed" },
    );
  });
});
