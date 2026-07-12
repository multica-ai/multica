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
import { configureShortcutPlatform, detectShortcutPlatform } from "./platform";

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
