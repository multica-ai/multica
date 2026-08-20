import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createShortcutChord,
  formatShortcut,
  isShortcutAllowedForAction,
  isReservedShortcut,
  parseLegacyShortcut,
  SHORTCUT_ACTIONS,
  SHORTCUT_ACTION_BY_ID,
  shortcutChordEquals,
  shortcutFromEvent,
  shortcutMatchesEvent,
} from "./definitions";
import {
  configureShortcutPlatform,
  detectShortcutPlatform,
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

  it("ships at most one action per default binding", () => {
    const seen: { id: string; shortcut: ReturnType<typeof createShortcutChord> }[] = [];
    for (const action of SHORTCUT_ACTIONS) {
      const shortcut = action.defaultShortcut;
      if (!shortcut) continue;
      const clash = seen.find((other) => shortcutChordEquals(other.shortcut, shortcut));
      expect(clash?.id, `${action.id} duplicates ${clash?.id}`).toBeUndefined();
      seen.push({ id: action.id, shortcut });
    }
  });

  it("keeps the floating chat toggle usable on every platform", () => {
    const action = SHORTCUT_ACTION_BY_ID.toggleChat;
    expect(action.defaultShortcut).toEqual(
      createShortcutChord("J", { primary: true }),
    );
    for (const platform of ["macos", "windows", "linux"] as const) {
      expect(
        isShortcutAllowedForAction(
          "toggleChat",
          createShortcutChord("J", { primary: true }),
          platform,
        ),
        `Mod+J must stay assignable on ${platform}`,
      ).toBe(true);
    }
    // Dismissing chat has to work with the caret inside its own composer.
    expect(action.allowInEditable).toBe(true);
  });

  it("keeps the inbox archive key out of editable controls", () => {
    const action = SHORTCUT_ACTION_BY_ID.archiveInboxItem;
    expect(action.defaultShortcut).toEqual(createShortcutChord("E"));
    expect(action.allowInEditable).toBe(false);
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

  it("reserves browser-owned accelerators", () => {
    for (const key of ["P", "L", "T", "N", "D", "U"]) {
      const chord = createShortcutChord(key, { primary: true });
      expect(isReservedShortcut(chord, "macos")).toBe(true);
      expect(isReservedShortcut(chord, "windows")).toBe(true);
      for (const extra of [{ shift: true }, { alt: true }, { control: true }]) {
        const variant = createShortcutChord(key, { primary: true, ...extra });
        expect(isReservedShortcut(variant, "macos")).toBe(true);
      }
    }
  });

  it("keeps browser and editing accelerators reserved", () => {
    for (const key of ["W", "R", "Q", "A", "C", "V", "X", "Y", "Z", "0", "Minus", "Plus"]) {
      expect(
        isReservedShortcut(createShortcutChord(key, { primary: true }), "macos"),
      ).toBe(true);
    }
    expect(isReservedShortcut(createShortcutChord("F5"), "windows")).toBe(true);
    expect(
      isReservedShortcut(createShortcutChord("Space", { primary: true }), "macos"),
    ).toBe(true);
  });

  it("reserves bare Cmd/Ctrl+P for browser print", () => {
    const cmdP = createShortcutChord("P", { primary: true });
    expect(isShortcutAllowedForAction("openSearch", cmdP, "macos")).toBe(false);
    expect(isShortcutAllowedForAction("goIssues", cmdP, "windows")).toBe(false);
    // Modifier variants stay reserved as well.
    expect(
      isShortcutAllowedForAction(
        "openSearch",
        createShortcutChord("P", { primary: true, shift: true }),
        "macos",
      ),
    ).toBe(false);
    // Send stays locked to Enter / Mod+Enter.
    expect(isShortcutAllowedForAction("send", cmdP, "macos")).toBe(false);
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
