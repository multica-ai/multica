// @vitest-environment node
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
  mods: Partial<Pick<ShortcutInput, "control" | "meta" | "alt" | "shift">> = {},
  code = /^[0-9]$/.test(k)
    ? `Digit${k}`
    : /^[a-z]$/i.test(k)
      ? `Key${k.toUpperCase()}`
      : "",
): ShortcutInput {
  return {
    type: "keyDown",
    key: k,
    code,
    control: false,
    meta: false,
    alt: false,
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
    expect(handleAppShortcut(key("+", { meta: true, shift: true }), wc, "darwin")).toBe(true);
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
    expect(handleAppShortcut(key("_", { meta: true, shift: true }), wc, "darwin")).toBe(true);
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

describe("handleAppShortcut — direct tab selection (Cmd/Ctrl+1..9)", () => {
  it.each([1, 2, 3, 4, 5, 6, 7, 8, 9] as const)(
    "maps Cmd+%i on macOS",
    (shortcutKey) => {
      const wc = makeWc();
      expect(
        handleAppShortcut(
          key(String(shortcutKey), { meta: true }),
          wc,
          "darwin",
        ),
      ).toEqual({ action: "select-tab", key: shortcutKey });
      expect(wc.setZoomLevel).not.toHaveBeenCalled();
    },
  );

  it.each(["linux", "win32"] as const)(
    "maps Ctrl+1..9 on %s",
    (platform) => {
      const wc = makeWc();
      for (let shortcutKey = 1; shortcutKey <= 9; shortcutKey += 1) {
        expect(
          handleAppShortcut(
            key(String(shortcutKey), { control: true }),
            wc,
            platform,
          ),
        ).toEqual({ action: "select-tab", key: shortcutKey });
      }
    },
  );

  it("does not capture a missing primary modifier or the wrong platform modifier", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("1"), wc, "darwin")).toBe(false);
    expect(
      handleAppShortcut(key("1", { control: true }), wc, "darwin"),
    ).toBe(false);
    expect(
      handleAppShortcut(key("1", { meta: true }), wc, "win32"),
    ).toBe(false);
  });

  it("accepts layout-required Shift while rejecting secondary modifiers", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        key("1", { meta: true, shift: true }, "Digit1"),
        wc,
        "darwin",
      ),
    ).toEqual({ action: "select-tab", key: 1 });
    expect(
      handleAppShortcut(key("1", { meta: true, alt: true }), wc, "darwin"),
    ).toBe(false);
    expect(
      handleAppShortcut(
        key("1", { meta: true, control: true }),
        wc,
        "darwin",
      ),
    ).toBe(false);
  });

  it("does not consume logical punctuation from a physical number-row key", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        key("&", { control: true }, "Digit1"),
        wc,
        "win32",
      ),
    ).toBe(false);
    expect(
      handleAppShortcut(
        key("ç", { control: true }, "Digit9"),
        wc,
        "win32",
      ),
    ).toBe(false);
    expect(wc.setZoomLevel).not.toHaveBeenCalled();
  });

  it.each([1, 2, 3, 4, 5, 6, 7, 8, 9] as const)(
    "maps Numpad%i when its logical key is numeric",
    (shortcutKey) => {
      const wc = makeWc();
      expect(
        handleAppShortcut(
          key(
            String(shortcutKey),
            { control: true },
            `Numpad${shortcutKey}`,
          ),
          wc,
          "linux",
        ),
      ).toEqual({ action: "select-tab", key: shortcutKey });
    },
  );

  it("does not consume a nonnumeric numpad key", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        key("End", { control: true }, "Numpad1"),
        wc,
        "win32",
      ),
    ).toBe(false);
  });

  it("uses a logical digit even when the physical key was remapped", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        key("1", { meta: true }, "KeyA"),
        wc,
        "darwin",
      ),
    ).toEqual({ action: "select-tab", key: 1 });
  });

  it("swallows auto-repeat without issuing another selection", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        { ...key("9", { meta: true }), isAutoRepeat: true },
        wc,
        "darwin",
      ),
    ).toBe(true);
  });

  it("keeps Cmd/Ctrl+0 assigned to zoom reset", () => {
    const wc = makeWc(2);
    expect(handleAppShortcut(key("0", { meta: true }), wc, "darwin")).toBe(
      true,
    );
    expect(wc.currentLevel()).toBe(0);
  });
});

describe("handleAppShortcut — unrelated keys pass through", () => {
  it("does not capture plain letters", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key("a", { meta: true }), wc, "darwin")).toBe(false);
    expect(handleAppShortcut(key("k", { meta: true }), wc, "darwin")).toBe(false);
  });

  it("rejects extra secondary modifiers for owned shortcuts", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(key("w", { meta: true, control: true }), wc, "darwin"),
    ).toBe(false);
    expect(
      handleAppShortcut(key("-", { control: true, alt: true }), wc, "win32"),
    ).toBe(false);
  });
});

describe("handleAppShortcut — open settings (Cmd/Ctrl+,)", () => {
  it('returns "open-settings" on Cmd+, (macOS)', () => {
    const wc = makeWc();
    expect(handleAppShortcut(key(",", { meta: true }), wc, "darwin")).toBe(
      "open-settings",
    );
  });

  it('returns "open-settings" on Ctrl+, (Linux/Windows)', () => {
    const wc = makeWc();
    expect(handleAppShortcut(key(",", { control: true }), wc, "linux")).toBe(
      "open-settings",
    );
    expect(handleAppShortcut(key(",", { control: true }), wc, "win32")).toBe(
      "open-settings",
    );
  });

  it("does not trigger without Cmd/Ctrl modifier", () => {
    const wc = makeWc();
    expect(handleAppShortcut(key(","), wc, "darwin")).toBe(false);
  });

  it("does not trigger with extra modifiers", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(key(",", { meta: true, alt: true }), wc, "darwin"),
    ).toBe(false);
    expect(
      handleAppShortcut(key(",", { meta: true, shift: true }), wc, "darwin"),
    ).toBe(false);
  });

  it("swallows auto-repeat without queuing another request", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        { ...key(",", { meta: true }), isAutoRepeat: true },
        wc,
        "darwin",
      ),
    ).toBe(true);
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

  it("swallows auto-repeat without closing additional tabs", () => {
    const wc = makeWc();
    expect(
      handleAppShortcut(
        { ...key("w", { meta: true }), isAutoRepeat: true },
        wc,
        "darwin",
      ),
    ).toBe(true);
  });
});
