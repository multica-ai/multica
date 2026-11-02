import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";

let editor: Editor;

afterEach(() => {
  editor?.destroy();
  document.body.innerHTML = "";
});

function load(markdown: string) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: createEditorExtensions({
      disableMentions: true,
      onUploadFileRef: { current: undefined },
    }),
    content: markdown,
    contentType: "markdown",
  });
}

function selectText(text: string) {
  let position: number | undefined;
  editor.state.doc.descendants((node, pos) => {
    if (node.isText && node.text === text) position = pos;
  });
  if (position === undefined) throw new Error(`Missing text: ${text}`);
  editor.commands.setTextSelection(position);
}

function press(key: string, shiftKey = false) {
  const event = new KeyboardEvent("keydown", {
    key,
    shiftKey,
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
  return event.defaultPrevented;
}

function itemAncestors() {
  const { $from } = editor.state.selection;
  const names: string[] = [];
  for (let depth = 1; depth <= $from.depth; depth++) {
    const name = $from.node(depth).type.name;
    if (name === "listItem" || name === "taskItem") names.push(name);
  }
  return names;
}

describe("list shortcuts through the production keymaps", () => {
  it.each(["-", "1.", "- [ ]"])("indents and outdents a %s item", (marker) => {
    load(`${marker} first\n${marker} second`);
    selectText("second");
    expect(press("Tab")).toBe(true);
    expect(itemAncestors()).toHaveLength(2);
    expect(press("Tab", true)).toBe(true);
    expect(itemAncestors()).toHaveLength(1);
  });

  it.each(["-", "1."])("indents a %s item nested under a checkbox", (marker) => {
    load(`- [ ] outer\n   ${marker} first\n   ${marker} second`);
    selectText("second");
    expect(press("Tab")).toBe(true);
    expect(itemAncestors()).toEqual(["taskItem", "listItem", "listItem"]);
    expect(press("Tab", true)).toBe(true);
    expect(itemAncestors()).toEqual(["taskItem", "listItem"]);
  });

  it("outdents an inner bullet without lifting its enclosing checkbox", () => {
    load("- [ ] outer\n   - first\n      - second");
    selectText("second");
    expect(press("Tab", true)).toBe(true);
    expect(itemAncestors()).toEqual(["taskItem", "listItem"]);
    expect(editor.getJSON().content?.[0]?.type).toBe("taskList");
  });

  it("splits an inner bullet without splitting its enclosing checkbox", () => {
    load("- [ ] outer\n   - first\n   - second");
    selectText("second");
    expect(press("Enter")).toBe(true);
    expect(itemAncestors()).toEqual(["taskItem", "listItem"]);
    expect(editor.getJSON().content?.[0]?.content).toHaveLength(1);
  });

  it("keeps the first nested bullet in place instead of indenting its enclosing checkbox", () => {
    load("- [ ] previous\n- [ ] outer\n   - first\n   - second");
    selectText("first");
    const before = editor.getJSON();
    expect(press("Tab")).toBe(true);
    expect(editor.getJSON()).toEqual(before);
  });

  it("indents and outdents a checkbox nested under a bullet", () => {
    load("- outer\n   - [ ] first\n   - [ ] second");
    selectText("second");
    expect(press("Tab")).toBe(true);
    expect(itemAncestors()).toEqual(["listItem", "taskItem", "taskItem"]);
    expect(press("Tab", true)).toBe(true);
    expect(itemAncestors()).toEqual(["listItem", "taskItem"]);
  });

  it("indents a nested whole-list selection, preserves Markdown, and undoes in one step", () => {
    load("- [ ] outer\n   - first\n   - second\n   - third");
    selectText("first");
    const from = editor.state.selection.from;
    selectText("third");
    editor.commands.setTextSelection({ from, to: editor.state.selection.from + 5 });
    const before = editor.getJSON();

    expect(press("Tab")).toBe(true);
    const indented = editor.getJSON();
    const markdown = editor.getMarkdown();
    expect(markdown.trim()).toBe("- [ ] outer\n   - first\n      - second\n      - third");
    expect(editor.commands.undo()).toBe(true);
    expect(editor.getJSON()).toEqual(before);
    editor.commands.setContent(markdown, { contentType: "markdown" });
    expect(editor.getJSON()).toEqual(indented);
  });

  it("leaves Tab available for focus navigation in plain text", () => {
    load("plain");
    selectText("plain");
    expect(press("Tab")).toBe(false);
    expect(press("Tab", true)).toBe(false);
  });

  it("preserves table cell navigation", () => {
    load("| first | second |\n| --- | --- |\n| third | fourth |");
    selectText("first");
    expect(press("Tab")).toBe(true);
    expect(editor.state.selection.$from.parent.textContent).toBe("second");
    expect(press("Tab", true)).toBe(true);
    expect(editor.state.selection.$from.parent.textContent).toBe("first");
  });
});
