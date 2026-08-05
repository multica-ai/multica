import { describe, expect, it } from "vitest";
import { isPickerAcceptKey, pickerNavigationDirection } from "./picker-keys";

describe("isPickerAcceptKey", () => {
  const accepts = (init: KeyboardEventInit) =>
    isPickerAcceptKey(new KeyboardEvent("keydown", init));

  it("treats Enter and plain Tab as accept keys", () => {
    expect(accepts({ key: "Enter" })).toBe(true);
    expect(accepts({ key: "Tab" })).toBe(true);
  });

  it("keeps Enter an accept key regardless of modifiers (Mod-Enter unchanged)", () => {
    expect(accepts({ key: "Enter", metaKey: true })).toBe(true);
  });

  it("does not treat Shift+Tab or Ctrl/Cmd/Alt+Tab as accept keys", () => {
    expect(accepts({ key: "Tab", shiftKey: true })).toBe(false);
    expect(accepts({ key: "Tab", ctrlKey: true })).toBe(false);
    expect(accepts({ key: "Tab", metaKey: true })).toBe(false);
    expect(accepts({ key: "Tab", altKey: true })).toBe(false);
  });

  it("ignores unrelated keys", () => {
    expect(accepts({ key: "ArrowDown" })).toBe(false);
    expect(accepts({ key: "Escape" })).toBe(false);
    expect(accepts({ key: "a" })).toBe(false);
  });
});

describe("pickerNavigationDirection", () => {
  const direction = (init: KeyboardEventInit) =>
    pickerNavigationDirection(new KeyboardEvent("keydown", init));

  it("maps the arrow keys regardless of modifiers", () => {
    expect(direction({ key: "ArrowDown" })).toBe("next");
    expect(direction({ key: "ArrowUp" })).toBe("prev");
    expect(direction({ key: "ArrowDown", ctrlKey: true })).toBe("next");
    expect(direction({ key: "ArrowUp", metaKey: true })).toBe("prev");
  });

  // The set cmdk's `vimBindings` gives the command bar for free; the pickers
  // must accept exactly the same chords.
  it("maps Ctrl+N/J to next and Ctrl+P/K to prev", () => {
    expect(direction({ key: "n", ctrlKey: true })).toBe("next");
    expect(direction({ key: "j", ctrlKey: true })).toBe("next");
    expect(direction({ key: "p", ctrlKey: true })).toBe("prev");
    expect(direction({ key: "k", ctrlKey: true })).toBe("prev");
  });

  it("still maps the letter aliases with Caps Lock on", () => {
    expect(direction({ key: "N", ctrlKey: true })).toBe("next");
    expect(direction({ key: "K", ctrlKey: true })).toBe("prev");
  });

  it("ignores the letters without Ctrl so typing a query is never hijacked", () => {
    expect(direction({ key: "n" })).toBe(null);
    expect(direction({ key: "j" })).toBe(null);
    expect(direction({ key: "p" })).toBe(null);
    expect(direction({ key: "k" })).toBe(null);
  });

  // Cmd+P prints and Cmd+N opens a window: browser/OS accelerators the app does
  // not own on web, and chords cmdk never bound either.
  it("does not treat Cmd+P/N as navigation", () => {
    expect(direction({ key: "p", metaKey: true })).toBe(null);
    expect(direction({ key: "n", metaKey: true })).toBe(null);
  });

  it("leaves Ctrl chords that carry another modifier alone", () => {
    expect(direction({ key: "n", ctrlKey: true, shiftKey: true })).toBe(null);
    expect(direction({ key: "n", ctrlKey: true, altKey: true })).toBe(null);
    expect(direction({ key: "n", ctrlKey: true, metaKey: true })).toBe(null);
  });

  it("ignores unrelated keys", () => {
    expect(direction({ key: "Enter" })).toBe(null);
    expect(direction({ key: "Escape" })).toBe(null);
    expect(direction({ key: "a", ctrlKey: true })).toBe(null);
  });
});
