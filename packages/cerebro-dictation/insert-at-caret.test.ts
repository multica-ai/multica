import { describe, expect, it, vi } from "vitest";
import { insertAtCaret } from "./insert-at-caret";

describe("insertAtCaret", () => {
  it("returns false for null target", () => {
    expect(insertAtCaret(null, "hi")).toBe(false);
  });

  it("returns false for empty text", () => {
    const ta = document.createElement("textarea");
    expect(insertAtCaret(ta, "")).toBe(false);
  });

  it("inserts at the cursor inside a textarea", () => {
    const ta = document.createElement("textarea");
    document.body.appendChild(ta);
    ta.value = "hello world";
    ta.setSelectionRange(5, 5);

    const onInput = vi.fn();
    ta.addEventListener("input", onInput);

    expect(insertAtCaret(ta, " brave")).toBe(true);
    expect(ta.value).toBe("hello brave world");
    expect(ta.selectionStart).toBe(11);
    expect(ta.selectionEnd).toBe(11);
    expect(onInput).toHaveBeenCalledTimes(1);

    ta.remove();
  });

  it("replaces the current selection inside a textarea", () => {
    const ta = document.createElement("textarea");
    ta.value = "hello world";
    ta.setSelectionRange(6, 11);

    expect(insertAtCaret(ta, "everyone")).toBe(true);
    expect(ta.value).toBe("hello everyone");
    expect(ta.selectionStart).toBe(14);
  });

  it("inserts into a text input", () => {
    const input = document.createElement("input");
    input.type = "text";
    input.value = "abc";
    input.setSelectionRange(3, 3);

    expect(insertAtCaret(input, "def")).toBe(true);
    expect(input.value).toBe("abcdef");
    expect(input.selectionStart).toBe(6);
  });

  it("skips non-text input types", () => {
    const input = document.createElement("input");
    input.type = "number";

    expect(insertAtCaret(input, "1")).toBe(false);
  });

  it("inserts into a contenteditable element", () => {
    const div = document.createElement("div");
    div.setAttribute("contenteditable", "true");
    div.textContent = "hello";
    document.body.appendChild(div);

    // jsdom's execCommand is unimplemented; force the fallback path via
    // Range mutation by stubbing it to return false.
    const original = document.execCommand;
    document.execCommand = vi.fn(() => false);

    const range = document.createRange();
    range.setStart(div.firstChild!, 5);
    range.setEnd(div.firstChild!, 5);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);

    expect(insertAtCaret(div, " world")).toBe(true);
    expect(div.textContent).toBe("hello world");

    document.execCommand = original;
    div.remove();
  });

  it("returns false for a non-editable element", () => {
    const div = document.createElement("div");
    expect(insertAtCaret(div, "x")).toBe(false);
  });
});
