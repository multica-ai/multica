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
  configureShortcutRuntime,
  detectShortcutPlatform,
  detectShortcutRuntime,
  getShortcutRuntime,
} from "./platform";

function keyEvent(
  key: string,
  fields: Partial<
    Pick<
      KeyboardEvent,
      "code" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey"
    >
  > = {},
): KeyboardEvent {
  return {
    key,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...fields,
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

  it("keeps the floating chat toggle usable on every platform and runtime", () => {
    const action = SHORTCUT_ACTION_BY_ID.toggleChat;
    expect(action.defaultShortcut).toEqual(
      createShortcutChord("J", { primary: true }),
    );
    for (const platform of ["macos", "windows", "linux"] as const) {
      for (const runtime of ["web", "desktop"] as const) {
        expect(
          isShortcutAllowedForAction(
            "toggleChat",
            createShortcutChord("J", { primary: true }),
            platform,
            runtime,
          ),
          `Mod+J must stay assignable on ${platform}/${runtime}`,
        ).toBe(true);
      }
    }
    // Dismissing chat has to work with the caret inside its own composer.
    expect(action.allowInEditable).toBe(true);
  });

  it("keeps the new-chat shortcut usable on every platform and runtime", () => {
    const action = SHORTCUT_ACTION_BY_ID.newChat;
    const chord = createShortcutChord("J", { primary: true, shift: true });
    expect(action.defaultShortcut).toEqual(chord);
    for (const platform of ["macos", "windows", "linux"] as const) {
      for (const runtime of ["web", "desktop"] as const) {
        expect(
          isShortcutAllowedForAction("newChat", chord, platform, runtime),
          `Mod+Shift+J must stay assignable on ${platform}/${runtime}`,
        ).toBe(true);
      }
    }
    expect(action.allowInEditable).toBe(true);
    expect(shortcutMatchesEvent(chord, keyEvent("J", { metaKey: true, shiftKey: true }), "macos")).toBe(true);
    expect(shortcutMatchesEvent(chord, keyEvent("j", { metaKey: true }), "macos")).toBe(false);
  });

  it("keeps the plain inbox archive key out of editable controls", () => {
    expect(SHORTCUT_ACTION_BY_ID.archiveInboxItem.allowInEditable).toBe(false);
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

  it("reserves the preferences chord on every runtime", () => {
    // Desktop opens Settings from the main process (before-input-event) and
    // browsers open their own settings, so Mod+, can never be recorded for a
    // product action.
    const chord = createShortcutChord(",", { primary: true });
    expect(isReservedShortcut(chord, "macos", "desktop")).toBe(true);
    expect(isReservedShortcut(chord, "macos", "web")).toBe(true);
    expect(isReservedShortcut(chord, "windows", "desktop")).toBe(true);
    expect(isShortcutAllowedForAction("goSettings", chord, "macos", "desktop")).toBe(false);
    // Only with the primary modifier — a bare comma stays typeable.
    expect(isReservedShortcut(createShortcutChord(","), "macos", "desktop")).toBe(false);
  });

  it("reserves browser-style tab selection on every platform and runtime", () => {
    for (const key of ["1", "2", "3", "4", "5", "6", "7", "8", "9"]) {
      const chord = createShortcutChord(key, { primary: true });
      for (const platform of ["macos", "windows", "linux"] as const) {
        for (const runtime of ["web", "desktop"] as const) {
          expect(isReservedShortcut(chord, platform, runtime)).toBe(true);
          expect(
            isShortcutAllowedForAction("openSearch", chord, platform, runtime),
          ).toBe(false);
        }
      }
    }
    expect(
      isReservedShortcut(createShortcutChord("1"), "macos", "desktop"),
    ).toBe(false);
  });

  it("models layout-sensitive number-row chords by their logical key", () => {
    const ampersand = shortcutFromEvent(
      keyEvent("&", { code: "Digit1", ctrlKey: true }),
      "windows",
    );
    expect(ampersand).toEqual(
      createShortcutChord("&", { primary: true }),
    );
    expect(isReservedShortcut(ampersand!, "windows", "desktop")).toBe(
      false,
    );

    const shiftedDigit = shortcutFromEvent(
      keyEvent("1", { code: "Digit1", ctrlKey: true, shiftKey: true }),
      "windows",
    );
    expect(shiftedDigit).toEqual(
      createShortcutChord("1", { primary: true, shift: true }),
    );
    expect(isReservedShortcut(shiftedDigit!, "windows", "desktop")).toBe(
      true,
    );
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

  it("frees modifier variants of reserved keys that nothing else assigns", () => {
    const free = [
      createShortcutChord("X", { primary: true, shift: true }),
      createShortcutChord("Y", { primary: true, shift: true }),
      createShortcutChord("A", { primary: true, alt: true }),
      createShortcutChord("Z", { primary: true, alt: true }),
      createShortcutChord("H", { primary: true, shift: true }),
      createShortcutChord("M", { primary: true, shift: true }),
    ];
    for (const chord of free) {
      for (const runtime of ["web", "desktop"] as const) {
        expect(
          isShortcutAllowedForAction("goInbox", chord, "macos", runtime),
          `${formatShortcut(chord, "macos")} on macos/${runtime}`,
        ).toBe(true);
      }
    }
    expect(
      isShortcutAllowedForAction(
        "openSearch",
        createShortcutChord("X", { primary: true, shift: true }),
        "windows",
        "web",
      ),
    ).toBe(true);
  });

  it("keeps the variants the OS, desktop menu or browser assign", () => {
    const everywhere = [
      createShortcutChord("Z", { primary: true, shift: true }),
      createShortcutChord("V", { primary: true, shift: true }),
      createShortcutChord("V", { primary: true, alt: true, shift: true }),
      createShortcutChord("H", { primary: true, alt: true }),
      createShortcutChord("M", { primary: true, alt: true }),
      createShortcutChord("F", { primary: true, control: true }),
      createShortcutChord("W", { primary: true, alt: true }),
      createShortcutChord("3", { primary: true, shift: true }),
    ];
    for (const chord of everywhere) {
      for (const runtime of ["web", "desktop"] as const) {
        expect(
          isReservedShortcut(chord, "macos", runtime),
          `${formatShortcut(chord, "macos")} on macos/${runtime}`,
        ).toBe(true);
      }
    }
  });

  it("reserves Electron's DevTools accelerator on desktop only", () => {
    const macDevTools = createShortcutChord("I", { primary: true, alt: true });
    expect(isReservedShortcut(macDevTools, "macos", "desktop")).toBe(true);
    expect(isShortcutAllowedForAction("openSearch", macDevTools, "macos", "web")).toBe(true);
    const devTools = createShortcutChord("I", { primary: true, shift: true });
    for (const platform of ["windows", "linux"] as const) {
      expect(isReservedShortcut(devTools, platform, "desktop")).toBe(true);
      expect(isShortcutAllowedForAction("openSearch", devTools, platform, "web")).toBe(true);
    }
    // Browser DevTools chords a page can claim stay recordable.
    expect(
      isReservedShortcut(createShortcutChord("C", { primary: true, shift: true }), "macos", "web"),
    ).toBe(false);
    expect(
      isReservedShortcut(createShortcutChord("J", { primary: true, alt: true }), "macos", "web"),
    ).toBe(false);
  });

  it("names macOS Option chords by physical key instead of the composed glyph", () => {
    expect(
      shortcutFromEvent(
        keyEvent("©", { code: "KeyG", metaKey: true, altKey: true }),
        "macos",
      ),
    ).toEqual(createShortcutChord("G", { primary: true, alt: true }));
    // Option+E/I/N/U/` are dead keys on the US layout, with or without Command.
    expect(
      shortcutFromEvent(keyEvent("Dead", { code: "KeyE", altKey: true }), "macos"),
    ).toEqual(createShortcutChord("E", { alt: true }));
    expect(
      shortcutFromEvent(keyEvent("¡", { code: "Digit1", metaKey: true, altKey: true }), "macos"),
    ).toEqual(createShortcutChord("1", { primary: true, alt: true }));
    expect(
      shortcutFromEvent(keyEvent("\u00a0", { code: "Space", ctrlKey: true, altKey: true }), "macos"),
    ).toEqual(createShortcutChord("Space", { control: true, alt: true }));
    // Named keys and ASCII output keep their logical key.
    expect(
      shortcutFromEvent(keyEvent("ArrowUp", { code: "ArrowUp", altKey: true }), "macos"),
    ).toEqual(createShortcutChord("Up", { alt: true }));
    // Option chords now reach the reserved policy with their real key.
    expect(
      isReservedShortcut(
        shortcutFromEvent(keyEvent("ˆ", { code: "KeyI", metaKey: true, altKey: true }), "macos")!,
        "macos",
        "desktop",
      ),
    ).toBe(true);
  });

  it("keeps matching Option chords saved under their composed glyph", () => {
    const event = keyEvent("©", { code: "KeyG", metaKey: true, altKey: true });
    expect(
      shortcutMatchesEvent(createShortcutChord("©", { primary: true, alt: true }), event, "macos"),
    ).toBe(true);
    expect(
      shortcutMatchesEvent(createShortcutChord("G", { primary: true, alt: true }), event, "macos"),
    ).toBe(true);
    expect(
      shortcutMatchesEvent(createShortcutChord("G", { primary: true }), event, "macos"),
    ).toBe(false);
  });

  it("treats AltGraph as Option on macOS but as character input elsewhere", () => {
    const altGraph = (fields: Parameters<typeof keyEvent>[1]) => ({
      ...keyEvent("©", { code: "KeyG", altKey: true, ...fields }),
      getModifierState: (modifier: string) => modifier === "AltGraph",
    }) as KeyboardEvent;
    expect(shortcutFromEvent(altGraph({ metaKey: true }), "macos")).toEqual(
      createShortcutChord("G", { primary: true, alt: true }),
    );
    expect(shortcutFromEvent(altGraph({ ctrlKey: true }), "windows")).toBeNull();
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

  it("lets voice input use Option chords that do not type a character", () => {
    const allowed = (key: string, modifiers: Parameters<typeof createShortcutChord>[1]) =>
      isShortcutAllowedForAction("voiceInput", createShortcutChord(key, modifiers), "macos", "web");
    expect(allowed("Space", { alt: true })).toBe(true);
    expect(allowed("F8", { alt: true })).toBe(true);
    expect(allowed("G", { primary: true, shift: true })).toBe(true);
    // Option+letter types a character on macOS.
    expect(allowed("V", { alt: true })).toBe(false);
    expect(allowed("Space", {})).toBe(false);
    expect(allowed("Space", { shift: true })).toBe(false);
    // Spotlight.
    expect(allowed("Space", { primary: true })).toBe(false);
    expect(SHORTCUT_ACTION_BY_ID.voiceInput.defaultShortcut).toBeNull();
    // macOS reports Option+Space as a non-breaking space.
    expect(
      shortcutMatchesEvent(
        createShortcutChord("Space", { alt: true }),
        keyEvent("\u00a0", { altKey: true }),
        "macos",
      ),
    ).toBe(true);
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
