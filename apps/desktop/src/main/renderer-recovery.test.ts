import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { createElectronReloadPrompt, installRendererRecoveryHandlers } from "./renderer-recovery";

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
    const desktopRoute = {
      surface: "tab",
      path: "/acme/issues/MUL-3239",
      workspaceSlug: "acme",
      tabId: "tab-1",
      reportedAt: "2026-06-15T00:00:00.000Z",
    };

    installRendererRecoveryHandlers(fixture.window, {
      isDev: false,
      showReloadPrompt,
      getDiagnosticContext: () => ({
        windowUrl:
          "file:///Applications/Multica.app/Contents/Resources/app.asar/index.html",
        desktopRoute,
      }),
      unresponsivePromptDelayMs: 100,
    });

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(showReloadPrompt).toHaveBeenCalledWith({
      kind: "unresponsive",
      context: {
        windowUrl:
          "file:///Applications/Multica.app/Contents/Resources/app.asar/index.html",
        desktopRoute,
      },
    });
    expect(fixture.reload).not.toHaveBeenCalled();
  });

  it("keeps prompting when diagnostic context collection fails", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const showReloadPrompt = vi.fn(async () => "dismiss" as const);

    installRendererRecoveryHandlers(fixture.window, {
      isDev: false,
      showReloadPrompt,
      getDiagnosticContext: () => {
        throw new Error("diagnostics unavailable");
      },
      unresponsivePromptDelayMs: 100,
    });

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(showReloadPrompt).toHaveBeenCalledWith({ kind: "unresponsive", context: {} });
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

  it("shows actionable recovery guidance before diagnostic details", async () => {
    let detail = "";
    const showMessageBox = vi.fn(
      async (options: { title: string; message: string; detail: string }) => {
        detail = options.detail;
        return { response: 1 };
      },
    );
    const showReloadPrompt = createElectronReloadPrompt(showMessageBox);

    await showReloadPrompt({ kind: "unresponsive", context: {} });

    expect(showMessageBox).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Multica needs to reload",
        message: "The desktop window has been stuck for a few seconds.",
        detail: expect.stringContaining(
          "Click Reload to refresh this window and keep using Multica.",
        ),
      }),
    );
    expect(detail).toContain("what you were doing right before this message appeared");
    expect(detail).toContain("Activity Monitor sample");
    expect(detail).toContain("Diagnostic details:\nkind: unresponsive\ncontext: {}");
  });
});

describe("freeze/crash breadcrumb state machine", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.useRealTimers());

  function install(fixture: ReturnType<typeof makeWindow>) {
    const persistBreadcrumb = vi.fn();
    const clearBreadcrumb = vi.fn();
    installRendererRecoveryHandlers(fixture.window, {
      isDev: false,
      showReloadPrompt: vi.fn(async () => "dismiss" as const),
      persistBreadcrumb,
      clearBreadcrumb,
      unresponsivePromptDelayMs: 100,
    });
    return { persistBreadcrumb, clearBreadcrumb };
  }

  it("a sustained hang writes exactly one unresponsive breadcrumb", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const { persistBreadcrumb, clearBreadcrumb } = install(fixture);

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(persistBreadcrumb).toHaveBeenCalledTimes(1);
    expect(persistBreadcrumb).toHaveBeenCalledWith(
      expect.objectContaining({ kind: "unresponsive" }),
    );
    expect(clearBreadcrumb).not.toHaveBeenCalled();
  });

  it("recovering after a written breadcrumb clears it (no double-count, no false recovered:false)", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const { persistBreadcrumb, clearBreadcrumb } = install(fixture);

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);
    expect(persistBreadcrumb).toHaveBeenCalledTimes(1);

    fixture.windowHandlers.get("responsive")?.();
    expect(clearBreadcrumb).toHaveBeenCalledTimes(1);
  });

  it("recovering before the delay never writes a breadcrumb, so nothing to clear", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const { persistBreadcrumb, clearBreadcrumb } = install(fixture);

    fixture.windowHandlers.get("unresponsive")?.();
    fixture.windowHandlers.get("responsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    expect(persistBreadcrumb).not.toHaveBeenCalled();
    expect(clearBreadcrumb).not.toHaveBeenCalled();
  });

  it("a hang that never recovers (force-quit) keeps its breadcrumb for next-boot reporting", async () => {
    vi.useFakeTimers();
    const fixture = makeWindow();
    const { persistBreadcrumb, clearBreadcrumb } = install(fixture);

    fixture.windowHandlers.get("unresponsive")?.();
    await vi.advanceTimersByTimeAsync(100);

    // No "responsive" ever fires — the breadcrumb must survive uncleared.
    expect(persistBreadcrumb).toHaveBeenCalledTimes(1);
    expect(clearBreadcrumb).not.toHaveBeenCalled();
  });

  it("a recoverable crash writes a breadcrumb and never clears it (a dead process never recovers)", () => {
    const fixture = makeWindow();
    const { persistBreadcrumb, clearBreadcrumb } = install(fixture);

    fixture.webContentsHandlers.get("render-process-gone")?.({}, { reason: "crashed" });

    expect(persistBreadcrumb).toHaveBeenCalledTimes(1);
    expect(persistBreadcrumb).toHaveBeenCalledWith(
      expect.objectContaining({ kind: "render-process-gone" }),
    );
    expect(clearBreadcrumb).not.toHaveBeenCalled();
  });

  it("a clean (non-crash) renderer exit writes no breadcrumb", () => {
    const fixture = makeWindow();
    const { persistBreadcrumb } = install(fixture);

    fixture.webContentsHandlers.get("render-process-gone")?.({}, { reason: "clean-exit" });

    expect(persistBreadcrumb).not.toHaveBeenCalled();
  });
});
