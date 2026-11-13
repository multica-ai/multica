import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from "../extensions";
import { insertDictation } from "./insert-dictation";

let editor: Editor | undefined;

function makeEditor(content?: string): Editor {
  const el = document.createElement("div");
  document.body.appendChild(el);
  editor = new Editor({
    element: el,
    extensions: createEditorExtensions({}),
    content,
    contentType: content ? "markdown" : undefined,
  });
  return editor;
}

function markdown(ed: Editor): string {
  return ed.getMarkdown().trim();
}

afterEach(() => {
  editor?.destroy();
  editor = undefined;
  document.body.innerHTML = "";
});

describe("insertDictation", () => {
  it("fills an empty editor", () => {
    const ed = makeEditor();
    insertDictation(ed, "Ship the fix today", false);
    expect(markdown(ed)).toBe("Ship the fix today");
  });

  it("appends to the last paragraph with a space when the caret was never placed", () => {
    const ed = makeEditor("First line\n\nSecond line");
    insertDictation(ed, "and more", false);
    expect(markdown(ed)).toBe("First line\n\nSecond line and more");
    insertDictation(ed, "then done", false);
    expect(markdown(ed)).toBe("First line\n\nSecond line and more then done");
  });

  it("does not put a space between Chinese characters", () => {
    const ed = makeEditor("今天");
    insertDictation(ed, "发布修复", false);
    expect(markdown(ed)).toBe("今天发布修复");
  });

  it("inserts at the caret and replaces a selection", () => {
    const ed = makeEditor("Hello world");
    // "Hello |world": position 1 is the paragraph start.
    ed.commands.setTextSelection({ from: 7, to: 12 });
    insertDictation(ed, "there", true);
    expect(markdown(ed)).toBe("Hello there");
  });

  it("keeps the following word separate mid-sentence and leaves the caret after the dictation", () => {
    const ed = makeEditor("Hello world");
    ed.commands.setTextSelection(7);
    insertDictation(ed, "brave", true);
    expect(markdown(ed)).toBe("Hello brave world");
    insertDictation(ed, "new", true);
    expect(markdown(ed)).toBe("Hello brave new world");
  });

  it("does not add a space before the punctuation that follows", () => {
    const ed = makeEditor("Hello, world");
    ed.commands.setTextSelection(6);
    insertDictation(ed, "there", true);
    expect(markdown(ed)).toBe("Hello there, world");
  });

  it("does not add spaces inside Chinese text", () => {
    const ed = makeEditor("今天发布");
    ed.commands.setTextSelection(3);
    insertDictation(ed, "我们", true);
    expect(markdown(ed)).toBe("今天我们发布");
  });

  it("collapses line breaks and inserts nothing for blank text", () => {
    const ed = makeEditor();
    insertDictation(ed, "one\n\ntwo\nthree", false);
    expect(markdown(ed)).toBe("one two three");
    insertDictation(ed, "  \n ", false);
    expect(markdown(ed)).toBe("one two three");
  });

  it("inserts markdown-looking text literally", () => {
    const ed = makeEditor();
    insertDictation(ed, "# not a heading", false);
    expect(ed.state.doc.firstChild?.type.name).toBe("paragraph");
  });
});
