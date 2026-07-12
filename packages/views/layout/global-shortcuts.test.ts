import { describe, expect, it } from "vitest";
import { shouldIgnoreGlobalShortcutEvent } from "./global-shortcuts";

describe("global shortcut event guard", () => {
  it("respects focused controls that already consumed the shortcut", () => {
    const event = new KeyboardEvent("keydown", {
      key: "k",
      metaKey: true,
      cancelable: true,
    });
    event.preventDefault();
    expect(shouldIgnoreGlobalShortcutEvent(event)).toBe(true);
  });

  it("ignores repeats and both standard and Safari IME composition events", () => {
    expect(
      shouldIgnoreGlobalShortcutEvent(
        new KeyboardEvent("keydown", { key: "k", repeat: true }),
      ),
    ).toBe(true);
    expect(
      shouldIgnoreGlobalShortcutEvent(
        new KeyboardEvent("keydown", { key: "k", isComposing: true }),
      ),
    ).toBe(true);

    const safariIme = new KeyboardEvent("keydown", { key: "k" });
    Object.defineProperty(safariIme, "keyCode", { value: 229 });
    expect(shouldIgnoreGlobalShortcutEvent(safariIme)).toBe(true);
  });

  it("allows a fresh unhandled keydown through", () => {
    expect(
      shouldIgnoreGlobalShortcutEvent(
        new KeyboardEvent("keydown", { key: "k", metaKey: true }),
      ),
    ).toBe(false);
  });
});
