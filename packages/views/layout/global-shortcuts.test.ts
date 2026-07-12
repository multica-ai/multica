import { describe, expect, it } from "vitest";
import {
  resolveCreateIssueDefaults,
  shouldIgnoreGlobalShortcutEvent,
} from "./global-shortcuts";

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

describe("create issue route defaults", () => {
  const spaces = [
    { id: "space-eng", key: "ENG" },
    { id: "space-design", key: "DES" },
  ];

  it("injects the current Space on Space child routes", () => {
    expect(
      resolveCreateIssueDefaults("/acme/space/eng/projects", spaces),
    ).toEqual({ space_id: "space-eng" });
  });

  it("preserves the project default on legacy project detail routes", () => {
    expect(resolveCreateIssueDefaults("/acme/projects/project-1", spaces)).toEqual(
      { project_id: "project-1" },
    );
  });

  it("does not invent a default outside a known context", () => {
    expect(resolveCreateIssueDefaults("/acme/chat", spaces)).toBeUndefined();
    expect(
      resolveCreateIssueDefaults("/acme/space/unknown/issues", spaces),
    ).toBeUndefined();
  });
});
