// @vitest-environment node

import { describe, expect, it, vi } from "vitest";
import { applyMainWindowCloseBehavior } from "./window-close-behavior";

function harness() {
  return {
    event: { preventDefault: vi.fn() },
    window: { hide: vi.fn(), minimize: vi.fn() },
    requestQuit: vi.fn(),
  };
}

describe("applyMainWindowCloseBehavior", () => {
  it("hides the main window without quitting for tray mode", () => {
    const ctx = harness();
    applyMainWindowCloseBehavior({ ...ctx, closeBehavior: "tray", isQuitting: false });
    expect(ctx.event.preventDefault).toHaveBeenCalledOnce();
    expect(ctx.window.hide).toHaveBeenCalledOnce();
    expect(ctx.window.minimize).not.toHaveBeenCalled();
    expect(ctx.requestQuit).not.toHaveBeenCalled();
  });

  it("minimizes the main window without hiding for taskbar mode", () => {
    const ctx = harness();
    applyMainWindowCloseBehavior({ ...ctx, closeBehavior: "taskbar", isQuitting: false });
    expect(ctx.event.preventDefault).toHaveBeenCalledOnce();
    expect(ctx.window.minimize).toHaveBeenCalledOnce();
    expect(ctx.window.hide).not.toHaveBeenCalled();
    expect(ctx.requestQuit).not.toHaveBeenCalled();
  });

  it("turns close into an explicit app quit for quit mode", () => {
    const ctx = harness();
    applyMainWindowCloseBehavior({ ...ctx, closeBehavior: "quit", isQuitting: false });
    expect(ctx.event.preventDefault).toHaveBeenCalledOnce();
    expect(ctx.requestQuit).toHaveBeenCalledOnce();
    expect(ctx.window.hide).not.toHaveBeenCalled();
    expect(ctx.window.minimize).not.toHaveBeenCalled();
  });

  it("does not intercept window teardown after an explicit quit starts", () => {
    const ctx = harness();
    applyMainWindowCloseBehavior({ ...ctx, closeBehavior: "tray", isQuitting: true });
    expect(ctx.event.preventDefault).not.toHaveBeenCalled();
    expect(ctx.window.hide).not.toHaveBeenCalled();
    expect(ctx.window.minimize).not.toHaveBeenCalled();
    expect(ctx.requestQuit).not.toHaveBeenCalled();
  });
});
