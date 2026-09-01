import { afterEach, describe, expect, it, vi } from "vitest";
import { desktopDictationAdapter } from "./dictation-adapter";

afterEach(() => vi.unstubAllGlobals());

describe("desktop dictation adapter", () => {
  it("fails closed when the installed preload has no native bridge", async () => {
    vi.stubGlobal("window", { desktopAPI: {} });
    expect(await desktopDictationAdapter.toggle()).toEqual({ ok: false, reason: "unavailable" });
  });

  it("delegates only a parameterless toggle without acquiring audio", async () => {
    const toggle = vi.fn().mockResolvedValue({ ok: true, shortcut: "Ctrl+Alt+Shift+D" });
    vi.stubGlobal("window", { desktopAPI: { dictation: { toggle } } });
    expect(await desktopDictationAdapter.toggle()).toMatchObject({ ok: true });
    expect(toggle).toHaveBeenCalledExactlyOnceWith();
  });
});
