import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { TaskList } from "@tiptap/extension-list";
import { PatchedListItem, PatchedTaskItem } from "./list-item";

interface JsonNode {
  type: string;
  text?: string;
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
      TaskList,
      PatchedTaskItem,
    ],
    content,
  });
}

/** Walk the doc and return the inside-paragraph position of the i-th listItem. */
function listItemTextPos(editor: Editor, index: number): number {
  let count = 0;
  let pos = -1;
  editor.state.doc.descendants((node, p) => {
    if (node.type.name === "listItem") {
      if (count === index) {
        pos = p + 2; // step over <listItem> + <paragraph> open
        return false;
      }
      count += 1;
    }
    return true;
  });
  if (pos < 0) throw new Error(`no listItem at index ${index}`);
  return pos;
}

/**
 * Mimic an editor keymap by invoking a bound shortcut directly. We can't drive
 * real key events reliably in jsdom, so we resolve the keymap an extension
 * registers and call the entry for `key`. The shared list keymap closes over
 * `editor` (not `this`), so the rebind only needs a faithful `this`.
 */
function pressShortcut(editor: Editor, extName: string, key: string): boolean {
  const ext = editor.extensionManager.extensions.find((e) => e.name === extName);
  if (!ext) throw new Error(`${extName} extension not registered`);
  const shortcuts = (
    ext.config.addKeyboardShortcuts as
      | (() => Record<string, () => boolean>)
      | undefined
  )?.bind({
    editor,
    name: extName,
    options: ext.options,
    type: editor.schema.nodes[extName],
    storage: ext.storage,
  } as never)();
  const fn = shortcuts?.[key];
  if (!fn) throw new Error(`${key} shortcut not bound on ${extName}`);
  return fn();
}

/** Mimic the editor's Enter keymap: invoke the bound Enter shortcut directly. */
function pressEnter(editor: Editor): boolean {
  return pressShortcut(editor, "listItem", "Enter");
}

/** Indented bullet outline of the doc — nesting depth, item text only. */
const LIST_TYPES = ["bulletList", "orderedList", "taskList"];
const ITEM_TYPES = ["listItem", "taskItem"];
function outline(json: JsonNode): string {
  const lines: string[] = [];
  function rec(node: JsonNode, depth: number) {
    for (const child of node.content ?? []) {
      if (LIST_TYPES.includes(child.type)) {
        rec(child, depth + 1);
      } else if (ITEM_TYPES.includes(child.type)) {
        const text = child.content?.[0]?.content?.[0]?.text ?? "";
        lines.push("  ".repeat(Math.max(0, depth - 1)) + "- " + text);
        for (const gc of child.content ?? []) {
          if (LIST_TYPES.includes(gc.type)) rec(gc, depth + 1);
        }
      } else {
        rec(child, depth);
      }
    }
  }
  rec(json, 0);
  return lines.join("\n");
}

/** Inside-paragraph position of the index-th item of `typeName` (doc order). */
function itemPos(editor: Editor, typeName: string, index: number): number {
  const positions: number[] = [];
  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === typeName) positions.push(pos + 2);
    return true;
  });
  const pos = positions[index];
  if (pos === undefined) throw new Error(`no ${typeName} at index ${index}`);
  return pos;
}

/** Select from the start of item `fromIdx`'s text to the end of item `toIdx`'s. */
function selectItemRange(
  editor: Editor,
  typeName: string,
  fromIdx: number,
  toIdx: number,
  itemLen = 3,
) {
  editor.commands.setTextSelection({
    from: itemPos(editor, typeName, fromIdx),
    to: itemPos(editor, typeName, toIdx) + itemLen,
  });
}

/** A flat three-item list ("aaa","bbb","ccc") of the given node types. */
function flatList(listType: string, itemType: string): JsonNode {
  return {
    type: "doc",
    content: [
      {
        type: listType,
        content: ["aaa", "bbb", "ccc"].map((t) => ({
          type: itemType,
          content: [{ type: "paragraph", content: [{ type: "text", text: t }] }],
        })),
      },
    ],
  };
}

describe("PatchedListItem Enter behaviour", () => {
  let editor: Editor | undefined;

  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("splits a non-empty list item into two", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                { type: "paragraph", content: [{ type: "text", text: "hello" }] },
              ],
            },
          ],
        },
      ],
    });

    // Cursor at end of "hello"
    editor.commands.setTextSelection(listItemTextPos(editor, 0) + 5);

    expect(pressEnter(editor)).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const list = json.content?.[0];
    expect(list?.type).toBe("bulletList");
    expect(list?.content).toHaveLength(2);
    const firstLiText =
      list?.content?.[0]?.content?.[0]?.content?.[0]?.text ?? "";
    expect(firstLiText).toBe("hello");
  });

  it("lifts an empty top-level list item out of the list (double-Enter exits)", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                { type: "paragraph", content: [{ type: "text", text: "first" }] },
              ],
            },
            { type: "listItem", content: [{ type: "paragraph" }] },
          ],
        },
      ],
    });

    // Cursor inside the empty second listItem
    editor.commands.setTextSelection(listItemTextPos(editor, 1));

    expect(pressEnter(editor)).toBe(true);

    const json = editor.getJSON() as JsonNode;
    // After lift, the bulletList holds only the first item; the empty li
    // becomes a sibling paragraph after the list.
    const list = json.content?.[0];
    const trailing = json.content?.[1];
    expect(list?.type).toBe("bulletList");
    expect(list?.content).toHaveLength(1);
    expect(trailing?.type).toBe("paragraph");
    expect(trailing?.content ?? []).toHaveLength(0);
  });

  it("splits a nested empty list item correctly (does not lift outer list)", () => {
    // doc > bulletList > listItem("outer") > bulletList > listItem("")
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                {
                  type: "paragraph",
                  content: [{ type: "text", text: "outer" }],
                },
                {
                  type: "bulletList",
                  content: [
                    { type: "listItem", content: [{ type: "paragraph" }] },
                  ],
                },
              ],
            },
          ],
        },
      ],
    });

    // Cursor in the inner empty list item (second listItem in doc order)
    editor.commands.setTextSelection(listItemTextPos(editor, 1));

    expect(pressEnter(editor)).toBe(true);

    // Behaviour: splitListItem's nested branch lifts the inner empty item
    // up one level — it becomes a new top-level listItem after the outer.
    // The outer listItem still exists with its "outer" text.
    const json = editor.getJSON() as JsonNode;
    const list = json.content?.[0];
    expect(list?.type).toBe("bulletList");
    const outer = list?.content?.[0];
    const outerText = outer?.content?.[0]?.content?.[0]?.text ?? "";
    expect(outerText).toBe("outer");
  });
});

describe("PatchedListItem Tab indent (MUL-3697)", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  const pressTab = (e: Editor) => pressShortcut(e, "listItem", "Tab");

  it("leaves the doc unchanged but swallows Tab in the first item (stay put, do not escape focus)", () => {
    editor = makeEditor(flatList("bulletList", "listItem"));
    editor.commands.setTextSelection(itemPos(editor, "listItem", 0));
    // Nothing to nest under, so the structural indent is a no-op — but the caret
    // is in a list, so Tab is swallowed (true) instead of leaking to the browser
    // and moving focus to other controls. The doc must be untouched.
    expect(pressTab(editor)).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe("- aaa\n- bbb\n- ccc");
  });

  it("indents a single non-first item under its predecessor", () => {
    editor = makeEditor(flatList("bulletList", "listItem"));
    editor.commands.setTextSelection(itemPos(editor, "listItem", 1));
    expect(pressTab(editor)).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe(
      "- aaa\n  - bbb\n- ccc",
    );
  });

  it("indents a whole-list selection (items 1..3): first stays, 2 and 3 nest", () => {
    editor = makeEditor(flatList("bulletList", "listItem"));
    selectItemRange(editor, "listItem", 0, 2);
    expect(pressTab(editor)).toBe(true);
    // The reported bug: this used to be a no-op because range.startIndex === 0.
    expect(outline(editor.getJSON() as JsonNode)).toBe(
      "- aaa\n  - bbb\n  - ccc",
    );
  });

  it("indents a mid-list selection (items 2..3) under the first", () => {
    editor = makeEditor(flatList("bulletList", "listItem"));
    selectItemRange(editor, "listItem", 1, 2);
    expect(pressTab(editor)).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe(
      "- aaa\n  - bbb\n  - ccc",
    );
  });

  it("returns false cleanly when the selection is not in a list (C2: no range)", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "plain" }] },
      ],
    });
    editor.commands.setTextSelection(3);
    expect(pressTab(editor)).toBe(false);
  });

  it("indents in a single, undoable transaction (C3)", () => {
    editor = makeEditor(flatList("bulletList", "listItem"));
    selectItemRange(editor, "listItem", 0, 2);

    const view = editor.view;
    const original = view.dispatch.bind(view);
    let dispatches = 0;
    view.dispatch = (tr) => {
      dispatches += 1;
      original(tr);
    };
    try {
      expect(pressTab(editor)).toBe(true);
    } finally {
      view.dispatch = original;
    }
    // One dispatch -> one transaction -> one undo step.
    expect(dispatches).toBe(1);

    editor.commands.undo();
    expect(outline(editor.getJSON() as JsonNode)).toBe("- aaa\n- bbb\n- ccc");
  });
});

describe("Tab indent across list types (MUL-3697)", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("indents an ordered-list whole selection (not only unordered)", () => {
    editor = makeEditor(flatList("orderedList", "listItem"));
    selectItemRange(editor, "listItem", 0, 2);
    expect(pressShortcut(editor, "listItem", "Tab")).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe(
      "- aaa\n  - bbb\n  - ccc",
    );
    expect((editor.getJSON() as JsonNode).content?.[0]?.type).toBe(
      "orderedList",
    );
  });

  it("indents a task-list whole selection via the taskItem keymap", () => {
    editor = makeEditor(flatList("taskList", "taskItem"));
    selectItemRange(editor, "taskItem", 0, 2);
    expect(pressShortcut(editor, "taskItem", "Tab")).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe(
      "- aaa\n  - bbb\n  - ccc",
    );
  });
});

describe("Shift-Tab dedent regression (MUL-3697)", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("lifts a multi-item nested selection back to the top level (unchanged)", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                { type: "paragraph", content: [{ type: "text", text: "aaa" }] },
                {
                  type: "bulletList",
                  content: [
                    {
                      type: "listItem",
                      content: [
                        {
                          type: "paragraph",
                          content: [{ type: "text", text: "bbb" }],
                        },
                      ],
                    },
                    {
                      type: "listItem",
                      content: [
                        {
                          type: "paragraph",
                          content: [{ type: "text", text: "ccc" }],
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          ],
        },
      ],
    });
    // Select the two nested items (bbb, ccc) and dedent.
    selectItemRange(editor, "listItem", 1, 2);
    expect(pressShortcut(editor, "listItem", "Shift-Tab")).toBe(true);
    expect(outline(editor.getJSON() as JsonNode)).toBe("- aaa\n- bbb\n- ccc");
  });
});
