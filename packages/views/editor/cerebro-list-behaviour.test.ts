import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { TaskList } from "@tiptap/extension-list";
import { Markdown } from "@tiptap/markdown";
import { useCerebroFeatureFlagsStore } from "@multica/cerebro-feature-flags";
import { PatchedListItem, PatchedTaskItem } from "./extensions/list-item";
import { createMarkdownPasteExtension } from "./extensions/markdown-paste";
import {
  CerebroListBehaviour,
  isListEditingEnabled,
} from "./cerebro-list-behaviour";

interface JsonNode {
  type: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: JsonNode[];
}

function makeEditor(content: JsonNode) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ listItem: false }),
      PatchedListItem,
      CerebroListBehaviour,
    ],
    content,
  });
}

/** Inside-paragraph position of the i-th listItem (step over <li> + <p> open). */
function listItemTextPos(editor: Editor, index: number): number {
  let count = 0;
  let pos = -1;
  editor.state.doc.descendants((node, p) => {
    if (pos >= 0) return false; // found already — `descendants` still visits siblings
    if (node.type.name === "listItem") {
      if (count === index) {
        pos = p + 2;
        return false;
      }
      count += 1;
    }
    return true;
  });
  if (pos < 0) throw new Error(`no listItem at index ${index}`);
  return pos;
}

function bullet(...items: string[]): JsonNode {
  return {
    type: "bulletList",
    content: items.map((text) => ({
      type: "listItem",
      content: [{ type: "paragraph", content: text ? [{ type: "text", text }] : [] }],
    })),
  };
}

/** Text of the top-level bulletList items, in order. */
function topItemTexts(editor: Editor): string[] {
  const json = editor.getJSON() as JsonNode;
  const list = json.content?.find((n) => n.type === "bulletList");
  return (list?.content ?? []).map(
    (li) => li.content?.[0]?.content?.[0]?.text ?? "",
  );
}

function paste(editor: Editor, text: string, html?: string): boolean {
  const event = {
    clipboardData: {
      files: [],
      getData: (type: string) =>
        type === "text/plain" ? text : type === "text/html" ? (html ?? "") : "",
    },
    preventDefault: () => {},
  } as unknown as ClipboardEvent;
  return (
    editor.view.someProp("handlePaste", (handler) =>
      handler(editor.view, event, editor.view.state.selection.content()),
    ) === true
  );
}

describe("CerebroListBehaviour — move item", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("moves an item down past its sibling", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b", "c")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0)); // in "a"
    expect(editor.commands.moveListItem(1)).toBe(true);
    expect(topItemTexts(editor)).toEqual(["b", "a", "c"]);
  });

  it("moves an item up past its sibling", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b", "c")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 2)); // in "c"
    expect(editor.commands.moveListItem(-1)).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "c", "b"]);
  });

  it("is a no-op at the top edge", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    expect(editor.commands.moveListItem(-1)).toBe(false);
    expect(topItemTexts(editor)).toEqual(["a", "b"]);
  });

  it("carries the item's nested children along", () => {
    // First item "a" owns a nested bullet list [a1]; move it down past "b".
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                { type: "paragraph", content: [{ type: "text", text: "a" }] },
                {
                  type: "bulletList",
                  content: [
                    {
                      type: "listItem",
                      content: [
                        { type: "paragraph", content: [{ type: "text", text: "a1" }] },
                      ],
                    },
                  ],
                },
              ],
            },
            {
              type: "listItem",
              content: [{ type: "paragraph", content: [{ type: "text", text: "b" }] }],
            },
          ],
        },
      ],
    });
    editor.commands.setTextSelection(listItemTextPos(editor, 0)); // in "a"
    expect(editor.commands.moveListItem(1)).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const list = json.content?.[0];
    const first = list?.content?.[0];
    const second = list?.content?.[1];
    // "b" is now first, "a" (with its nested a1) is second.
    expect(first?.content?.[0]?.content?.[0]?.text).toBe("b");
    expect(second?.content?.[0]?.content?.[0]?.text).toBe("a");
    const nested = second?.content?.[1];
    expect(nested?.type).toBe("bulletList");
    expect(nested?.content?.[0]?.content?.[0]?.content?.[0]?.text).toBe("a1");
  });
});

describe("CerebroListBehaviour — Backspace at start", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("lifts a top-level item to a paragraph when the cursor is at its start", () => {
    editor = makeEditor({ type: "doc", content: [bullet("hello")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0)); // start of "hello"
    expect(editor.commands.outdentListItemAtStart()).toBe(true);

    const json = editor.getJSON() as JsonNode;
    expect(json.content?.[0]?.type).toBe("paragraph");
    expect(json.content?.[0]?.content?.[0]?.text).toBe("hello");
  });

  it("does nothing when the cursor is not at the item start", () => {
    editor = makeEditor({ type: "doc", content: [bullet("hello")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0) + 2); // mid-word
    expect(editor.commands.outdentListItemAtStart()).toBe(false);
    expect(editor.getJSON().content?.[0]?.type).toBe("bulletList");
  });
});

describe("CerebroListBehaviour — multi-line paste into a list item", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("splits a multi-line plain-text paste into sibling items", () => {
    editor = makeEditor({ type: "doc", content: [bullet("")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    expect(paste(editor, "alpha\nbeta\ngamma")).toBe(true);
    expect(topItemTexts(editor)).toEqual(["alpha", "beta", "gamma"]);
  });

  it("keeps existing text and makes each pasted line a new sibling item", () => {
    editor = makeEditor({ type: "doc", content: [bullet("keep")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0) + 4); // end of "keep"
    expect(paste(editor, "alpha\nbeta")).toBe(true);
    expect(topItemTexts(editor)).toEqual(["keep", "alpha", "beta"]);
  });

  it("ignores a single-line paste (lets other handlers run)", () => {
    editor = makeEditor({ type: "doc", content: [bullet("")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    expect(paste(editor, "just one line")).toBe(false);
  });

  it("defers a pasted markdown list to the markdown paste handler", () => {
    editor = makeEditor({ type: "doc", content: [bullet("")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    // Lines that are themselves list markers belong to markdownPaste, which
    // preserves their indent; this handler must stand aside.
    expect(paste(editor, "- one\n- two")).toBe(false);
  });

  it("does nothing outside a list item", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "x" }] }],
    });
    editor.commands.setTextSelection(1);
    expect(paste(editor, "alpha\nbeta")).toBe(false);
  });
});

// The production list stack: PatchedListItem's Tab / Shift-Tab / Enter keymap,
// the checkbox TaskList / PatchedTaskItem input rules, StarterKit's `- ` / `* `
// / `1. ` rules, and markdownPaste — all alongside CerebroListBehaviour, so the
// slice-11 rules are covered against the real extension set, not a subset.
function makeFullEditor(content?: JsonNode) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ listItem: false }),
      PatchedListItem,
      CerebroListBehaviour,
      TaskList,
      PatchedTaskItem,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
      createMarkdownPasteExtension(),
    ],
    ...(content ? { content } : {}),
  });
}

/** Invoke the keyboard shortcut `key` bound by extension `extName`. */
function pressKey(editor: Editor, extName: string, key: string): boolean {
  const ext = editor.extensionManager.extensions.find((e) => e.name === extName);
  if (!ext) throw new Error(`extension ${extName} not registered`);
  const build = ext.config.addKeyboardShortcuts as
    | (() => Record<string, () => boolean>)
    | undefined;
  const shortcuts = build?.bind({
    editor,
    name: ext.name,
    options: ext.options,
    type: editor.schema.nodes[ext.name] ?? null,
    storage: ext.storage,
  } as never)();
  const handler = shortcuts?.[key];
  if (!handler) throw new Error(`${extName} does not bind ${key}`);
  return handler();
}

// Type each character through handleTextInput, so input rules fire exactly as
// they do on real keystrokes (setContent's markdown path bypasses them).
function typeText(editor: Editor, text: string) {
  for (const ch of text) {
    const { from, to } = editor.state.selection;
    const handled = editor.view.someProp("handleTextInput", (f) =>
      f(editor.view, from, to, ch, () => editor.state.tr),
    );
    if (!handled) editor.view.dispatch(editor.state.tr.insertText(ch, from, to));
  }
}

function findFirst(node: JsonNode, type: string): JsonNode | undefined {
  if (node.type === type) return node;
  for (const child of node.content ?? []) {
    const hit = findFirst(child, type);
    if (hit) return hit;
  }
  return undefined;
}

describe("CerebroListBehaviour — Tab / Shift-Tab indent and outdent", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("Tab sinks an item under its predecessor", () => {
    editor = makeFullEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1)); // in "b"
    expect(pressKey(editor, "listItem", "Tab")).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const top = findFirst(json, "bulletList");
    // One top-level item "a" now owns a nested bulletList holding "b".
    expect(top?.content).toHaveLength(1);
    const nested = top?.content?.[0]?.content?.[1];
    expect(nested?.type).toBe("bulletList");
    expect(nested?.content?.[0]?.content?.[0]?.content?.[0]?.text).toBe("b");
  });

  it("Shift-Tab lifts a nested item back to the top level", () => {
    editor = makeFullEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1));
    pressKey(editor, "listItem", "Tab"); // "b" now nested under "a"
    editor.commands.setTextSelection(listItemTextPos(editor, 1)); // back in "b"
    expect(pressKey(editor, "listItem", "Shift-Tab")).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "b"]);
  });
});

describe("CerebroListBehaviour — Enter on an empty item leaves the list", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("lifts an empty trailing item out to a plain paragraph", () => {
    editor = makeFullEditor({ type: "doc", content: [bullet("a", "")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1)); // in empty item
    expect(pressKey(editor, "listItem", "Enter")).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const list = json.content?.find((n) => n.type === "bulletList");
    expect(list?.content).toHaveLength(1); // only "a" remains in the list
    expect(json.content?.[json.content.length - 1]?.type).toBe("paragraph");
  });
});

describe("CerebroListBehaviour — repeated Backspace walks an item out", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("lifts a nested item one level per press, then to a paragraph", () => {
    editor = makeFullEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1));
    pressKey(editor, "listItem", "Tab"); // "b" nested under "a"

    // First Backspace at the start of "b": lift back to a top-level item.
    editor.commands.setTextSelection(listItemTextPos(editor, 1));
    expect(pressKey(editor, "cerebroListBehaviour", "Backspace")).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "b"]);

    // Second Backspace at the start: lift "b" out of the list to a paragraph.
    editor.commands.setTextSelection(listItemTextPos(editor, 1));
    expect(pressKey(editor, "cerebroListBehaviour", "Backspace")).toBe(true);
    const json = editor.getJSON() as JsonNode;
    const list = json.content?.find((n) => n.type === "bulletList");
    expect(list?.content).toHaveLength(1); // only "a" left in the list
    const para = json.content?.find(
      (n) => n.type === "paragraph" && n.content?.[0]?.text === "b",
    );
    expect(para).toBeDefined(); // "b" is now a plain paragraph, out of the list
  });
});

describe("CerebroListBehaviour — input rules", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("`- ` starts a bullet list", () => {
    editor = makeFullEditor();
    typeText(editor, "- item");
    expect(findFirst(editor.getJSON() as JsonNode, "bulletList")).toBeDefined();
  });

  it("`* ` starts a bullet list", () => {
    editor = makeFullEditor();
    typeText(editor, "* item");
    expect(findFirst(editor.getJSON() as JsonNode, "bulletList")).toBeDefined();
  });

  it("`1. ` starts an ordered list", () => {
    editor = makeFullEditor();
    typeText(editor, "1. item");
    expect(findFirst(editor.getJSON() as JsonNode, "orderedList")).toBeDefined();
  });

  it("`[] ` starts an unchecked task item", () => {
    editor = makeFullEditor();
    typeText(editor, "[] wash");
    const item = findFirst(editor.getJSON() as JsonNode, "taskItem");
    expect(item).toBeDefined();
    expect(item?.attrs?.checked).toBe(false);
  });

  it("`[x] ` starts a checked task item", () => {
    editor = makeFullEditor();
    typeText(editor, "[x] done");
    const item = findFirst(editor.getJSON() as JsonNode, "taskItem");
    expect(item).toBeDefined();
    expect(item?.attrs?.checked).toBe(true);
  });
});

describe("CerebroListBehaviour — feature gate", () => {
  afterEach(() => {
    useCerebroFeatureFlagsStore.setState({
      overrides: {},
      workspaceOverrides: {},
    });
  });

  it("is off by default (flag unregistered / unset)", () => {
    expect(isListEditingEnabled()).toBe(false);
  });

  it("turns on when cerebro_editor_toolbar is enabled personally", () => {
    useCerebroFeatureFlagsStore.setState({
      overrides: { cerebro_editor_toolbar: true } as Record<string, boolean>,
    });
    expect(isListEditingEnabled()).toBe(true);
  });

  it("turns on when the workspace enables it", () => {
    useCerebroFeatureFlagsStore.setState({
      workspaceOverrides: { cerebro_editor_toolbar: true } as Record<
        string,
        boolean
      >,
    });
    expect(isListEditingEnabled()).toBe(true);
  });
});

describe("CerebroListBehaviour — markdown list paste preserves indent", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("turns pasted markdown list text into a real nested list", () => {
    editor = makeFullEditor();
    editor.commands.setTextSelection(1);
    // Pasted markdown with an indented sub-item: markdownPaste owns this and
    // keeps the child nested under its parent.
    expect(paste(editor, "- parent\n   - child")).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const top = findFirst(json, "bulletList");
    expect(top?.content?.[0]?.content?.[0]?.content?.[0]?.text).toBe("parent");
    const nested = top?.content?.[0]?.content?.[1];
    expect(nested?.type).toBe("bulletList");
    expect(nested?.content?.[0]?.content?.[0]?.content?.[0]?.text).toBe("child");
  });
});
