import { afterEach, describe, expect, it } from "vitest";
import { createShortcutChord } from "./definitions";
import { configureShortcutPlatform, configureShortcutRuntime } from "./platform";
import {
  findShortcutConflict,
  getShortcut,
  migrateShortcutState,
  sanitizeShortcutOverrides,
  useShortcutStore,
} from "./store";

afterEach(() => {
  useShortcutStore.getState().resetAll();
  configureShortcutPlatform(null);
  configureShortcutRuntime(null);
});

describe("shortcut store", () => {
  it("uses defaults, persists overrides, and can disable an action", () => {
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("K", { primary: true }),
    );
    expect(getShortcut("send")).toEqual(
      createShortcutChord("Enter", { primary: true }),
    );

    const custom = createShortcutChord("J", { primary: true });
    useShortcutStore.getState().setShortcut("openSearch", custom);
    expect(getShortcut("openSearch")).toEqual(custom);

    useShortcutStore.getState().setShortcut("openSearch", null);
    expect(getShortcut("openSearch")).toBeNull();
  });

  it("removes structurally equal overrides when restoring a default", () => {
    useShortcutStore.getState().setShortcut(
      "createIssue",
      createShortcutChord("I", { primary: true }),
    );
    useShortcutStore.getState().setShortcut(
      "createIssue",
      createShortcutChord("C"),
    );

    expect(useShortcutStore.getState().overrides.createIssue).toBeUndefined();
  });

  it("rejects unsafe programmatic overrides as a store invariant", () => {
    useShortcutStore.getState().setShortcut(
      "send",
      createShortcutChord("C"),
    );
    useShortcutStore.getState().setShortcut(
      "send",
      createShortcutChord("Enter", { shift: true }),
    );
    useShortcutStore.getState().setShortcut(
      "send",
      createShortcutChord("Enter", { primary: true, shift: true }),
    );
    expect(getShortcut("send")).toEqual(
      createShortcutChord("Enter", { primary: true }),
    );
  });

  it("drops persisted Send overrides outside Enter and Mod+Enter", () => {
    expect(
      sanitizeShortcutOverrides({
        send: createShortcutChord("Enter", { shift: true }),
      }),
    ).toEqual({});
  });

  it("keeps browser-only bindings when the runtime is desktop", () => {
    const cmdP = createShortcutChord("P", { primary: true });

    configureShortcutRuntime("desktop");
    expect(sanitizeShortcutOverrides({ openSearch: cmdP })).toEqual({
      openSearch: cmdP,
    });
    useShortcutStore.getState().setShortcut("openSearch", cmdP);
    expect(getShortcut("openSearch")).toEqual(cmdP);

    // The same persisted value hydrating in a browser falls back to default.
    configureShortcutRuntime("web");
    expect(sanitizeShortcutOverrides({ openSearch: cmdP })).toEqual({});
  });

  it("keeps persisted punctuation overrides that remain reachable on desktop", () => {
    const ctrlAmpersand = createShortcutChord("&", { primary: true });

    configureShortcutRuntime("desktop");
    expect(
      sanitizeShortcutOverrides({ openSearch: ctrlAmpersand }),
    ).toEqual({ openSearch: ctrlAmpersand });
  });

  it("finds conflicts against defaults and overrides", () => {
    expect(
      findShortcutConflict(
        "createIssue",
        createShortcutChord("K", { primary: true }),
      ),
    ).toBe("openSearch");

    const custom = createShortcutChord("G", { primary: true });
    useShortcutStore.getState().setShortcut("goInbox", custom);
    expect(findShortcutConflict("goChat", custom)).toBe("goInbox");
  });

  it("treats an Option chord saved under its glyph as taken by the same keypress", () => {
    configureShortcutPlatform("macos");
    useShortcutStore.getState().setShortcut(
      "goInbox",
      createShortcutChord("©", { primary: true, alt: true }),
    );
    const event = {
      key: "©",
      code: "KeyG",
      metaKey: true,
      altKey: true,
      ctrlKey: false,
      shiftKey: false,
    } as KeyboardEvent;
    const physical = createShortcutChord("G", { primary: true, alt: true });
    expect(findShortcutConflict("goChat", physical)).toBeNull();
    expect(findShortcutConflict("goChat", physical, event)).toBe("goInbox");
  });

  it("keeps a persisted Ctrl+Shift+I override on web but not on desktop", () => {
    const devTools = createShortcutChord("I", { primary: true, shift: true });
    configureShortcutPlatform("windows");
    configureShortcutRuntime("web");
    expect(sanitizeShortcutOverrides({ openSearch: devTools })).toEqual({
      openSearch: devTools,
    });
    configureShortcutRuntime("desktop");
    expect(sanitizeShortcutOverrides({ openSearch: devTools })).toEqual({});
  });

  it("migrates v1 string overrides without losing disabled actions", () => {
    expect(
      migrateShortcutState({
        overrides: { openSearch: "Mod+Shift+J", createIssue: null },
      }, 1),
    ).toEqual({
      overrides: {
        openSearch: createShortcutChord("J", { primary: true, shift: true }),
        createIssue: null,
      },
    });
  });

  it("drops malformed current-version storage and unknown actions", () => {
    expect(
      sanitizeShortcutOverrides({
        openSearch: { key: "J", modifiers: { primary: true } },
        findInIssue: createShortcutChord("J"),
        send: createShortcutChord("Enter"),
        goInbox: null,
        goChat: createShortcutChord("Meta"),
        unknownAction: createShortcutChord("X"),
      }),
    ).toEqual({
      send: createShortcutChord("Enter"),
      goInbox: null,
    });

    expect(
      migrateShortcutState({ overrides: "corrupt" }, 2),
    ).toEqual({ overrides: {} });
  });

  it("falls back to defaults instead of disabling when legacy data is invalid", () => {
    expect(
      migrateShortcutState({
        overrides: {
          openSearch: "",
          createIssue: 42,
          goInbox: null,
          unknownAction: "Mod+X",
        },
      }, 1),
    ).toEqual({ overrides: { goInbox: null } });
  });
});
