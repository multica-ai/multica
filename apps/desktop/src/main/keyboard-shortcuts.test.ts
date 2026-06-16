import { describe, expect, it, vi } from "vitest";
import { handleAppShortcut, type ShortcutInput } from "./keyboard-shortcuts";

function makeWc(initialLevel = 0) {
  let level = initialLevel;
  return {
    getZoomLevel: vi.fn(() => level),
    setZoomLevel: vi.fn((next: number) => {
      level = next;
    }),
    currentLevel: () => level,
  };
}

function key(
  k: string,
  mods: Partial<Pick<ShortcutInput, "control" | "meta" | "shift">> = {},
): ShortcutInput {
  return {
    type: "keyDown",
    key: k,
    control: false,
    meta: false,
    shift: false,
    ...mods,
  };
}

describe("handleAppShortcut — reload blocking", () => {
  it("swallows Cmd+R on macOS", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("r", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.setZoomLevel).not.toHaveBeenCalled();
  });

  it("swallows Ctrl+R on Linux/Windows", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("r", { control: true }), wc, "linux")).toBe(true);
    expect(handleAppShortcut(key("R", { control: true }), wc, "win32")).toBe(true);
  });

  it("swallows F5 regardless of modifier", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("F5"), wc, "darwin")).toBe(true);
  });

  it("ignores non-keyDown events", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut({ ...key("r", { meta: true }), type: "keyUp" }, wc, "darwin"),
    ).toBe(false);
  });
});

describe("handleAppShortcut — zoom in", () => {
  it("zooms in on Cmd+= (unshifted)", () => {
    const wc = makeWc(0);
    expect(handleAppShortcut(key("=", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("zooms in on Cmd++ (Shift+=)", () => {
    const wc = makeWc(0);
    expect(handleAppShortcut(key("+", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("zooms in on Ctrl+= on non-mac", () => {
    const wc = makeWc(0);
    expect(handleAppShortcut(key("=", { control: true }), wc, "linux")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("does nothing without Cmd/Ctrl", () => {
    const wc = makeWc(0);
    expect(handleAppShortcut(key("="), wc, "darwin")).toBe(false);
    expect(wc.setZoomLevel).not.toHaveBeenCalled();
  });

  it("clamps zoom-in at the upper bound", () => {
    const wc = makeWc(4.5);
    expect(handleAppShortcut(key("=", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(4.5);
  });
});

describe("handleAppShortcut — zoom out (regression: MUL-2354)", () => {
  it("zooms out on Cmd+- (unshifted)", () => {
    const wc = makeWc(1);
    expect(handleAppShortcut(key("-", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("zooms out on Cmd+_ (Shift+-)", () => {
    const wc = makeWc(1);
    expect(handleAppShortcut(key("_", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("zooms out on Ctrl+- on non-mac", () => {
    const wc = makeWc(1);
    expect(handleAppShortcut(key("-", { control: true }), wc, "win32")).toBe(true);
    expect(wc.currentLevel()).toBe(0.5);
  });

  it("undoes a prior Cmd+= so the user can return to 100%", () => {
    const wc = makeWc(0);
    handleAppShortcut(key("=", { meta: true }), wc, "darwin");
    expect(wc.currentLevel()).toBe(0.5);
    handleAppShortcut(key("-", { meta: true }), wc, "darwin");
    expect(wc.currentLevel()).toBe(0);
  });

  it("clamps zoom-out at the lower bound", () => {
    const wc = makeWc(-3);
    expect(handleAppShortcut(key("-", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(-3);
  });

  it("does nothing without Cmd/Ctrl", () => {
    const wc = makeWc(1);
    expect(handleAppShortcut(key("-"), wc, "darwin")).toBe(false);
    expect(wc.setZoomLevel).not.toHaveBeenCalled();
  });
});

describe("handleAppShortcut — reset zoom", () => {
  it("resets to 0 on Cmd+0", () => {
    const wc = makeWc(2);
    expect(handleAppShortcut(key("0", { meta: true }), wc, "darwin")).toBe(true);
    expect(wc.currentLevel()).toBe(0);
  });

  it("resets to 0 on Ctrl+0", () => {
    const wc = makeWc(-1.5);
    expect(handleAppShortcut(key("0", { control: true }), wc, "linux")).toBe(true);
    expect(wc.currentLevel()).toBe(0);
  });

  it("ignores plain 0 without modifier", () => {
    const wc = makeWc(2);
    expect(handleAppShortcut(key("0"), wc, "darwin")).toBe(false);
    expect(wc.setZoomLevel).not.toHaveBeenCalled();
  });
});

describe("handleAppShortcut — unrelated keys pass through", () => {
  it("does not capture plain letters", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("a", { meta: true }), wc, "darwin")).toBe(false);
    expect(handleAppShortcut(key("k", { meta: true }), wc, "darwin")).toBe(false);
  });
});

describe("handleAppShortcut — close tab (Cmd/Ctrl+W)", () => {
  it('returns "close-tab" on Cmd+W (macOS)', () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("w", { meta: true }), wc, "darwin")).toBe("close-tab");
  });

  it('returns "close-tab" on Cmd+W uppercase', () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("W", { meta: true }), wc, "darwin")).toBe("close-tab");
  });

  it('returns "close-tab" on Ctrl+W (Linux/Windows)', () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("w", { control: true }), wc, "linux")).toBe("close-tab");
    expect(handleAppShortcut(key("w", { control: true }), wc, "win32")).toBe("close-tab");
  });

  it("does not trigger without Cmd/Ctrl modifier", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("w"), wc, "darwin")).toBe(false);
  });

  it("does not trigger on Cmd+Shift+W (reserved for close-window)", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("W", { meta: true, shift: true }), wc, "darwin")).toBe(false);
  });

  it("does not trigger on Ctrl+Shift+W (reserved for close-window)", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("W", { control: true, shift: true }), wc, "linux")).toBe(false);
  });
});
