import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, renderHook } from "@testing-library/react";
import { createShortcutChord } from "@multica/core/shortcuts";
import {
  issuePropertyShortcutFromEvent,
  useIssuePropertyShortcuts,
} from "./use-issue-property-shortcuts";

afterEach(() => {
  document.body.replaceChildren();
});

describe("issue property shortcuts", () => {
  const statusShortcut = createShortcutChord("S");
  const priorityShortcut = createShortcutChord("P");

  it("matches configured bindings without consuming typing, modifiers, or popup keys", () => {
    const match = (event: KeyboardEvent) =>
      issuePropertyShortcutFromEvent(event, statusShortcut, priorityShortcut);
    expect(match(new KeyboardEvent("keydown", { key: "s" }))).toBe("status");
    expect(match(new KeyboardEvent("keydown", { key: "P" }))).toBe("priority");
    expect(match(new KeyboardEvent("keydown", { key: "p", ctrlKey: true }))).toBeNull();
    expect(match(new KeyboardEvent("keydown", { key: "s", repeat: true }))).toBeNull();

    const input = document.createElement("input");
    document.body.append(input);
    expect(match(new KeyboardEvent("keydown", { key: "s", bubbles: true }))).toBe("status");
    const typed = new KeyboardEvent("keydown", { key: "s", bubbles: true });
    input.dispatchEvent(typed);
    expect(match(typed)).toBeNull();

    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.append(dialog);
    const dialogKey = new KeyboardEvent("keydown", { key: "p", bubbles: true });
    dialog.dispatchEvent(dialogKey);
    expect(match(dialogKey)).toBeNull();

    const rebound = createShortcutChord("Y", { alt: true });
    expect(issuePropertyShortcutFromEvent(
      new KeyboardEvent("keydown", { key: "y", altKey: true }),
      rebound,
      null,
    )).toBe("status");
    expect(issuePropertyShortcutFromEvent(
      new KeyboardEvent("keydown", { key: "s" }),
      rebound,
      null,
    )).toBeNull();
  });

  it("opens only from a visible mounted issue detail and cleans up the listener", () => {
    const target = document.createElement("div");
    const rects = vi.spyOn(target, "getClientRects").mockReturnValue({ length: 1 } as DOMRectList);
    const onOpen = vi.fn();
    const ref = { current: target };
    const { unmount } = renderHook(() => useIssuePropertyShortcuts(
      ref, true, statusShortcut, priorityShortcut, onOpen,
    ));

    fireEvent.keyDown(document.body, { key: "s" });
    expect(onOpen).toHaveBeenCalledWith("status");
    rects.mockReturnValue({ length: 0 } as DOMRectList);
    fireEvent.keyDown(document.body, { key: "p" });
    expect(onOpen).toHaveBeenCalledTimes(1);
    unmount();
    fireEvent.keyDown(document.body, { key: "s" });
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});
