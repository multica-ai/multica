import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { copyText } from "./clipboard";

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

function selectEditable(direction: "forward" | "backward" | "collapsed") {
  const editor = document.createElement("div");
  editor.contentEditable = "true";
  editor.tabIndex = 0;
  editor.textContent = "synthetic editable text";
  document.body.append(editor);
  editor.focus();
  const text = editor.firstChild!;
  const selection = document.getSelection()!;
  const anchor = direction === "backward" ? 12 : direction === "collapsed" ? 8 : 2;
  const focus = direction === "collapsed" ? 8 : direction === "backward" ? 2 : 12;
  selection.setBaseAndExtent(text, anchor, text, focus);
  return { editor, text, selection, anchor, focus };
}

beforeEach(() => {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: undefined,
  });
  // jsdom does not move DOM Selection when a textarea receives focus. Model
  // that browser behavior so these tests exercise restoration, not a no-op.
  const focus = HTMLTextAreaElement.prototype.focus;
  vi.spyOn(HTMLTextAreaElement.prototype, "focus").mockImplementation(
    function (this: HTMLTextAreaElement) {
      focus.call(this);
      document.getSelection()?.removeAllRanges();
    },
  );
  Object.defineProperty(document, "execCommand", {
    configurable: true,
    value: vi.fn(() => true),
  });
});

afterEach(() => {
  document.body.replaceChildren();
  document.getSelection()?.removeAllRanges();
  vi.restoreAllMocks();
  if (clipboardDescriptor) {
    Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  } else {
    Reflect.deleteProperty(navigator, "clipboard");
  }
});

describe("copyText selection preservation", () => {
  it.each(["forward", "backward", "collapsed"] as const)("restores a %s contenteditable selection after fallback", async (direction) => {
    const { editor, text, selection, anchor, focus } = selectEditable(direction);
    expect(await copyText("synthetic code payload")).toBe(true);
    expect(document.activeElement).toBe(editor);
    expect(selection.anchorNode).toBe(text);
    expect(selection.anchorOffset).toBe(anchor);
    expect(selection.focusNode).toBe(text);
    expect(selection.focusOffset).toBe(focus);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it.each(["false", "throw"])("restores selection when execCommand returns %s", async (outcome) => {
    const { text, selection } = selectEditable("backward");
    vi.mocked(document.execCommand).mockImplementation(() => {
      if (outcome === "throw") throw new Error("synthetic copy failure");
      return false;
    });
    expect(await copyText("payload")).toBe(false);
    expect(selection.anchorNode).toBe(text);
    expect(selection.anchorOffset).toBe(12);
    expect(selection.focusOffset).toBe(2);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("restores selection when async writeText rejects into fallback", async () => {
    const { text, selection } = selectEditable("forward");
    const writeText = vi.fn().mockRejectedValue(new Error("synthetic denial"));
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    expect(await copyText("payload")).toBe(true);
    expect(writeText).toHaveBeenCalledWith("payload");
    expect(selection.anchorNode).toBe(text);
    expect(selection.anchorOffset).toBe(2);
    expect(selection.focusOffset).toBe(12);
  });

  it("leaves selection and focus untouched on async success", async () => {
    const { editor, text, selection } = selectEditable("backward");
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    expect(await copyText("payload")).toBe(true);
    expect(document.execCommand).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(editor);
    expect(selection.anchorNode).toBe(text);
    expect(selection.anchorOffset).toBe(12);
    expect(selection.focusOffset).toBe(2);
  });

  it.each(["input", "textarea"] as const)("preserves the focused %s selection", async (tag) => {
    const input = document.createElement(tag);
    input.value = "synthetic form field";
    document.body.append(input);
    input.focus();
    input.setSelectionRange(2, 9, "backward");
    expect(await copyText("payload")).toBe(true);
    expect(document.activeElement).toBe(input);
    expect([input.selectionStart, input.selectionEnd, input.selectionDirection]).toEqual([2, 9, "backward"]);
    expect(document.querySelectorAll("textarea")).toHaveLength(tag === "textarea" ? 1 : 0);
  });

  it("keeps an empty DOM selection empty and restores the button focus", async () => {
    const button = document.createElement("button");
    document.body.append(button);
    button.focus();
    document.getSelection()!.removeAllRanges();
    expect(await copyText("payload")).toBe(true);
    expect(document.getSelection()!.rangeCount).toBe(0);
    expect(document.activeElement).toBe(button);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("does not restore a selection or focus into disconnected nodes", async () => {
    const { editor, text, selection } = selectEditable("forward");
    const focus = vi.spyOn(editor, "focus");
    vi.mocked(document.execCommand).mockImplementation(() => {
      editor.remove();
      return true;
    });
    expect(await copyText("payload")).toBe(true);
    expect(selection.anchorNode).not.toBe(text);
    expect(document.activeElement).toBe(document.body);
    expect(focus).not.toHaveBeenCalled();
    expect(document.querySelector("textarea")).toBeNull();
  });
});
