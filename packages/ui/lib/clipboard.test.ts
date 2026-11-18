import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { canCopyImage, copyImage, copyText, copyTextAndHtml } from "./clipboard";

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

describe("copyTextAndHtml", () => {
  class FakeClipboardItem {
    constructor(readonly data: Record<string, Blob>) {}
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("writes the plain-text and HTML channels in one clipboard item", async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { write },
      configurable: true,
    });
    vi.stubGlobal("ClipboardItem", FakeClipboardItem);

    expect(
      await copyTextAndHtml(
        "[TES-1 Example](https://app.example/test/issues/TES-1)",
        '<a href="https://app.example/test/issues/TES-1">TES-1 Example</a>',
      ),
    ).toBe(true);

    expect(write).toHaveBeenCalledTimes(1);
    const items = (write.mock.calls[0] as [FakeClipboardItem[]])[0]!;
    expect(items).toHaveLength(1);
    const item = items[0]!;
    const plain = item.data["text/plain"]!;
    const html = item.data["text/html"]!;
    expect(plain.type).toBe("text/plain");
    expect(await plain.text()).toBe(
      "[TES-1 Example](https://app.example/test/issues/TES-1)",
    );
    expect(html.type).toBe("text/html");
    expect(await html.text()).toBe(
      '<a href="https://app.example/test/issues/TES-1">TES-1 Example</a>',
    );
  });

  it("falls back to the plain-text channel when the rich write is rejected", async () => {
    const write = vi.fn().mockRejectedValue(new Error("synthetic denial"));
    Object.defineProperty(navigator, "clipboard", {
      value: { write },
      configurable: true,
    });
    vi.stubGlobal("ClipboardItem", FakeClipboardItem);

    expect(await copyTextAndHtml("plain", "<b>rich</b>")).toBe(true);
    expect(document.execCommand).toHaveBeenCalledWith("copy");
  });

  it("falls back to the plain-text channel when ClipboardItem is unavailable", async () => {
    const write = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      value: { write, writeText: vi.fn().mockResolvedValue(undefined) },
      configurable: true,
    });
    vi.stubGlobal("ClipboardItem", undefined);

    expect(await copyTextAndHtml("plain", "<b>rich</b>")).toBe(true);
    expect(write).not.toHaveBeenCalled();
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("plain");
  });
});

describe("copyImage", () => {
  class FakeClipboardItem {
    constructor(readonly data: Record<string, Promise<Blob>>) {}
  }

  // Like the browser, the write settles only once the item's data has.
  function stubClipboard() {
    const written: Blob[] = [];
    const write = vi.fn(async (items: FakeClipboardItem[]) => {
      written.push(await items[0]!.data["image/png"]!);
    });
    Object.defineProperty(navigator, "clipboard", { value: { write }, configurable: true });
    vi.stubGlobal("ClipboardItem", FakeClipboardItem);
    return { write, written };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("copies the PNG a loader returns, starting the write before it settles", async () => {
    const { write, written } = stubClipboard();
    const png = new Blob(["png"], { type: "image/png" });
    let resolve!: (blob: Blob) => void;
    const loader = vi.fn(() => new Promise<Blob>((r) => (resolve = r)));

    const copied = copyImage(loader);
    expect(loader).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledTimes(1);
    resolve(png);

    expect(await copied).toBe(true);
    expect(written).toEqual([png]);
  });

  it("is unavailable without the async Clipboard API, as on plain http://", async () => {
    vi.stubGlobal("ClipboardItem", FakeClipboardItem);
    const loader = vi.fn(async () => new Blob(["png"], { type: "image/png" }));
    expect(canCopyImage()).toBe(false);
    expect(await copyImage(loader)).toBe(false);
    expect(loader).not.toHaveBeenCalled();

    stubClipboard();
    expect(canCopyImage()).toBe(true);
  });

  it("fails when the loader does", async () => {
    stubClipboard();
    expect(await copyImage(() => Promise.reject(new Error("synthetic 404")))).toBe(false);
  });
});
