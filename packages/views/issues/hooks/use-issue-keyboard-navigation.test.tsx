import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  isIssueKeyboardShortcutTarget,
  nextIssueKeyboardIndex,
  useIssueKeyboardNavigation,
} from "./use-issue-keyboard-navigation";

function dispatchKey(key: string, target: Document | HTMLElement = document) {
  target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
}

describe("nextIssueKeyboardIndex", () => {
  it("starts from the first item when moving down with no active issue", () => {
    expect(nextIssueKeyboardIndex(null, ["a", "b"], 1)).toBe(0);
  });

  it("starts from the last item when moving up with no active issue", () => {
    expect(nextIssueKeyboardIndex(null, ["a", "b"], -1)).toBe(1);
  });

  it("clamps at the list bounds", () => {
    expect(nextIssueKeyboardIndex("a", ["a", "b"], -1)).toBe(0);
    expect(nextIssueKeyboardIndex("b", ["a", "b"], 1)).toBe(1);
  });
});

describe("isIssueKeyboardShortcutTarget", () => {
  it("ignores editable targets and overlays", () => {
    const input = document.createElement("input");
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    const button = document.createElement("button");
    dialog.append(button);
    document.body.append(input, dialog);

    expect(isIssueKeyboardShortcutTarget(input)).toBe(true);
    expect(isIssueKeyboardShortcutTarget(button)).toBe(true);
    expect(isIssueKeyboardShortcutTarget(document.body)).toBe(false);
  });
});

describe("useIssueKeyboardNavigation", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      callback(performance.now());
      return 1;
    });
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("moves through issues with j/k and opens the active issue with Enter", () => {
    document.body.innerHTML = `
      <div data-issue-keyboard-id="a"></div>
      <div data-issue-keyboard-id="b"></div>
      <div data-issue-keyboard-id="c"></div>
    `;
    const onOpenIssue = vi.fn();
    const { result } = renderHook(() =>
      useIssueKeyboardNavigation({ issueIds: ["a", "b", "c"], onOpenIssue }),
    );

    act(() => dispatchKey("j"));
    expect(result.current.activeIssueId).toBe("a");

    act(() => dispatchKey("j"));
    expect(result.current.activeIssueId).toBe("b");

    act(() => dispatchKey("k"));
    expect(result.current.activeIssueId).toBe("a");

    act(() => dispatchKey("Enter"));
    expect(onOpenIssue).toHaveBeenCalledWith("a");
  });

  it("does not navigate while typing in an input", () => {
    const input = document.createElement("input");
    document.body.append(input);
    const onOpenIssue = vi.fn();
    const { result } = renderHook(() =>
      useIssueKeyboardNavigation({ issueIds: ["a", "b"], onOpenIssue }),
    );

    act(() => dispatchKey("j", input));

    expect(result.current.activeIssueId).toBeNull();
  });

  it("syncs active issue when the visible issue list changes", () => {
    const onOpenIssue = vi.fn();
    const { result, rerender } = renderHook(
      ({ issueIds }) => useIssueKeyboardNavigation({ issueIds, onOpenIssue }),
      { initialProps: { issueIds: ["a", "b"] } },
    );

    act(() => dispatchKey("j"));
    expect(result.current.activeIssueId).toBe("a");

    rerender({ issueIds: ["b", "c"] });

    expect(result.current.activeIssueId).toBe("b");
  });
});
