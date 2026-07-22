import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createShortcutChord,
  formatShortcut,
  isShortcutAllowedForAction,
  isReservedShortcut,
  parseLegacyShortcut,
  SHORTCUT_ACTIONS,
  shortcutFromEvent,
  shortcutMatchesEvent,
} from "./definitions";
import {
  configureShortcutPlatform,
  configureShortcutRuntime,
  detectShortcutPlatform,
  detectShortcutRuntime,
  getShortcutRuntime,
} from "./platform";

function keyEvent(
  key: string,
  modifiers: Partial<Pick<KeyboardEvent, "metaKey" | "ctrlKey" | "altKey" | "shiftKey">> = {},
): KeyboardEvent {
  return {
    key,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...modifiers,
  } as KeyboardEvent;
}

afterEach(() => {
  configureShortcutPlatform(null);
  configureShortcutRuntime(null);
  vi.unstubAllGlobals();
});

describe("keyboard shortcut definitions", () => {
  it("keeps every shipped default inside the action safety policy", () => {
    for (const action of SHORTCUT_ACTIONS) {
      if (!action.defaultShortcut) continue;
      expect(
        isShortcutAllowedForAction(
          action.id,
          action.defaultShortcut,
          "macos",
        ),
      ).toBe(true);
      expect(
        isShortcutAllowedForAction(
          action.id,
          action.defaultShortcut,
          "windows",
        ),
      ).toBe(true);
    }
  });

  it("strictly distinguishes Command and Control on macOS", () => {
    const commandF = createShortcutChord("F", { primary: true });
    const controlF = createShortcutChord("F", { control: true });

    expect(shortcutMatchesEvent(commandF, keyEvent("f", { metaKey: true }), "macos")).toBe(true);
    expect(shortcutMatchesEvent(commandF, keyEvent("f", { ctrlKey: true }), "macos")).toBe(false);
    expect(shortcutMatchesEvent(controlF, keyEvent("f", { ctrlKey: true }), "macos")).toBe(true);
    expect(shortcutMatchesEvent(controlF, keyEvent("f", { metaKey: true }), "macos")).toBe(false);
  });

  it("maps Control to primary on Windows/Linux and keeps Meta separate", () => {
    const primaryK = createShortcutChord("K", { primary: true });
    expect(shortcutMatchesEvent(primaryK, keyEvent("k", { ctrlKey: true }), "windows")).toBe(true);
    expect(shortcutMatchesEvent(primaryK, keyEvent("k", { metaKey: true }), "windows")).toBe(false);
    expect(shortcutFromEvent(keyEvent("k", { metaKey: true }), "windows")).toEqual(
      createShortcutChord("K", { meta: true }),
    );
  });

  it("requires every modifier to match exactly", () => {
    const shortcut = createShortcutChord("K", { primary: true });
    expect(
      shortcutMatchesEvent(
        shortcut,
        keyEvent("k", { ctrlKey: true, shiftKey: true }),
        "linux",
      ),
    ).toBe(false);
  });

  it.each([
    ["Meta", { metaKey: true }],
    ["Control", { ctrlKey: true }],
    ["Alt", { altKey: true }],
    ["Shift", { shiftKey: true }],
  ])("never matches an unassigned action when %s is pressed alone", (key, modifiers) => {
    expect(shortcutFromEvent(keyEvent(key, modifiers), "macos")).toBeNull();
    expect(shortcutMatchesEvent(null, keyEvent(key, modifiers), "macos")).toBe(false);
  });

  it("ignores synthetic events without a key, such as Chrome autofill", () => {
    const event = keyEvent(undefined as unknown as string, { metaKey: true });
    expect(shortcutFromEvent(event, "macos")).toBeNull();
    expect(
      shortcutMatchesEvent(createShortcutChord("K", { primary: true }), event, "macos"),
    ).toBe(false);
  });

  it("formats the same semantic binding for each platform", () => {
    const shortcut = createShortcutChord("Enter", { primary: true });
    expect(formatShortcut(shortcut, "macos")).toBe("⌘↵");
    expect(formatShortcut(shortcut, "windows")).toBe("Ctrl+Enter");
    expect(formatShortcut(shortcut, "linux")).toBe("Ctrl+Enter");
  });

  it("detects modern and legacy browser platform signals", () => {
    vi.stubGlobal("navigator", {
      userAgentData: { platform: "macOS" },
      platform: "Win32",
      userAgent: "",
    });
    expect(detectShortcutPlatform()).toBe("macos");
  });

  it("falls back past empty or unrecognized platform signals", () => {
    vi.stubGlobal("navigator", {
      userAgentData: { platform: "" },
      platform: "",
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X)",
    });
    expect(detectShortcutPlatform()).toBe("macos");
  });

  it("uses platform-specific reserved shortcuts", () => {
    expect(
      isReservedShortcut(createShortcutChord("Space", { primary: true }), "macos"),
    ).toBe(true);
    expect(
      isReservedShortcut(createShortcutChord("K", { meta: true }), "windows"),
    ).toBe(true);
    expect(
      isReservedShortcut(createShortcutChord("K", { primary: true }), "windows"),
    ).toBe(false);
  });

  it("reserves browser-owned accelerators on web but frees the bare chords on desktop", () => {
    for (const key of ["P", "L", "T", "N", "D", "U"]) {
      const chord = createShortcutChord(key, { primary: true });
      expect(isReservedShortcut(chord, "macos", "web")).toBe(true);
      expect(isReservedShortcut(chord, "windows", "web")).toBe(true);
      expect(isReservedShortcut(chord, "macos", "desktop")).toBe(false);
      expect(isReservedShortcut(chord, "windows", "desktop")).toBe(false);
      // Only the bare primary chord opens up — extra modifiers keep the
      // historical reservation on every runtime.
      for (const extra of [{ shift: true }, { alt: true }, { control: true }]) {
        const variant = createShortcutChord(key, { primary: true, ...extra });
        expect(isReservedShortcut(variant, "macos", "desktop")).toBe(true);
        expect(isReservedShortcut(variant, "macos", "web")).toBe(true);
      }
    }
    // OS-owned combos that motivated the narrowing must stay reserved on
    // desktop: Option+Cmd+D toggles the macOS Dock, Ctrl+Alt+T opens a
    // terminal on common Linux desktops.
    expect(
      isReservedShortcut(
        createShortcutChord("D", { primary: true, alt: true }),
        "macos",
        "desktop",
      ),
    ).toBe(true);
    expect(
      isReservedShortcut(
        createShortcutChord("T", { primary: true, alt: true }),
        "linux",
        "desktop",
      ),
    ).toBe(true);
  });

  it("keeps app-owned and editing accelerators reserved on desktop", () => {
    for (const key of ["W", "R", "Q", "A", "C", "V", "X", "Y", "Z", "0", "Minus", "Plus"]) {
      expect(
        isReservedShortcut(createShortcutChord(key, { primary: true }), "macos", "desktop"),
      ).toBe(true);
    }
    expect(isReservedShortcut(createShortcutChord("F5"), "windows", "desktop")).toBe(true);
    expect(
      isReservedShortcut(createShortcutChord("Space", { primary: true }), "macos", "desktop"),
    ).toBe(true);
  });

  it("allows recording bare Cmd/Ctrl+P for an action on desktop only", () => {
    const cmdP = createShortcutChord("P", { primary: true });
    expect(isShortcutAllowedForAction("openSearch", cmdP, "macos", "desktop")).toBe(true);
    expect(isShortcutAllowedForAction("openSearch", cmdP, "macos", "web")).toBe(false);
    expect(isShortcutAllowedForAction("goIssues", cmdP, "windows", "desktop")).toBe(true);
    // Modifier variants keep the historical reservation even on desktop.
    expect(
      isShortcutAllowedForAction(
        "openSearch",
        createShortcutChord("P", { primary: true, shift: true }),
        "macos",
        "desktop",
      ),
    ).toBe(false);
    // Send stays locked to Enter / Mod+Enter regardless of runtime.
    expect(isShortcutAllowedForAction("send", cmdP, "macos", "desktop")).toBe(false);
  });

  it("detects the desktop runtime from preload globals with a configure override", () => {
    expect(detectShortcutRuntime()).toBe("web");
    vi.stubGlobal("window", { desktopAPI: {} });
    expect(detectShortcutRuntime()).toBe("desktop");
    vi.stubGlobal("window", { electron: {} });
    expect(detectShortcutRuntime()).toBe("desktop");
    vi.stubGlobal("window", {});
    expect(detectShortcutRuntime()).toBe("web");
    expect(getShortcutRuntime()).toBe("web");
    configureShortcutRuntime("desktop");
    expect(getShortcutRuntime()).toBe("desktop");
  });

  it("rejects modifier-only, composition-only, and unidentified key events", () => {
    for (const key of ["Fn", "CapsLock", "Dead", "Process", "Unidentified"]) {
      expect(shortcutFromEvent(keyEvent(key), "macos")).toBeNull();
    }
  });

  it("prevents unsafe plain keys from hijacking editors and global navigation", () => {
    expect(
      isShortcutAllowedForAction("openSearch", createShortcutChord("J"), "macos"),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction("send", createShortcutChord("J"), "macos"),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction(
        "send",
        createShortcutChord("J", { shift: true }),
        "macos",
      ),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction(
        "openSearch",
        createShortcutChord("J", { alt: true }),
        "macos",
      ),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction("send", createShortcutChord("Enter"), "macos"),
    ).toBe(true);
    expect(
      isShortcutAllowedForAction(
        "send",
        createShortcutChord("Enter", { primary: true }),
        "macos",
      ),
    ).toBe(true);
    expect(
      isShortcutAllowedForAction(
        "send",
        createShortcutChord("Enter", { shift: true }),
        "macos",
      ),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction(
        "send",
        createShortcutChord("Enter", { primary: true, shift: true }),
        "macos",
      ),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction("goInbox", createShortcutChord("Enter"), "macos"),
    ).toBe(false);
    expect(
      isShortcutAllowedForAction("goInbox", createShortcutChord("G"), "macos"),
    ).toBe(true);
  });

  it("protects fundamental editing shortcuts from reassignment", () => {
    expect(
      isShortcutAllowedForAction(
        "send",
        createShortcutChord("C", { primary: true }),
        "macos",
      ),
    ).toBe(false);
  });

  it("parses v1 persisted string bindings", () => {
    expect(parseLegacyShortcut("Mod+Shift+K")).toEqual(
      createShortcutChord("K", { primary: true, shift: true }),
    );
    expect(parseLegacyShortcut("Bogus+K")).toBeNull();
  });
});
