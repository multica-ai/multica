import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

beforeEach(() => {
  vi.resetModules();
});

describe("keyboard platform helper", () => {
  it("renders Mac symbols when navigator.platform is MacIntel", async () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    const mod = await import("./keyboard");

    expect(mod.isMac).toBe(true);
    expect(mod.modKey).toBe("⌘");
    expect(mod.enterKey).toBe("↵");
    expect(mod.formatShortcut(mod.modKey, "K")).toBe("⌘K");
    expect(mod.formatShortcut(mod.modKey, mod.enterKey)).toBe("⌘↵");
  });

  it("renders Ctrl/Enter on Windows", async () => {
    vi.stubGlobal("navigator", { platform: "Win32" });
    const mod = await import("./keyboard");

    expect(mod.isMac).toBe(false);
    expect(mod.modKey).toBe("Ctrl");
    expect(mod.enterKey).toBe("Enter");
    expect(mod.formatShortcut(mod.modKey, "K")).toBe("Ctrl+K");
    expect(mod.formatShortcut(mod.modKey, mod.enterKey)).toBe("Ctrl+Enter");
  });

  it("renders Ctrl/Enter on Linux", async () => {
    vi.stubGlobal("navigator", { platform: "Linux x86_64" });
    const mod = await import("./keyboard");

    expect(mod.isMac).toBe(false);
    expect(mod.modKey).toBe("Ctrl");
    expect(mod.formatShortcut("Ctrl", "Shift", "P")).toBe("Ctrl+Shift+P");
  });

  it("falls back to non-Mac when navigator is unavailable (SSR)", async () => {
    vi.stubGlobal("navigator", undefined);
    const mod = await import("./keyboard");

    expect(mod.isMac).toBe(false);
    expect(mod.modKey).toBe("Ctrl");
  });
});
