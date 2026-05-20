import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { PatchedListItem } from "./list-item";

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
    extensions: [StarterKit.configure({ listItem: false }), PatchedListItem],
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

/** Mimic the editor's Enter keymap: invoke the bound Enter shortcut directly. */
function pressEnter(editor: Editor): boolean {
  const listItemExt = editor.extensionManager.extensions.find(
    (e) => e.name === "listItem",
  );
  if (!listItemExt) throw new Error("listItem extension not registered");
  const shortcuts = (
    listItemExt.config.addKeyboardShortcuts as
      | (() => Record<string, () => boolean>)
      | undefined
  )?.bind({
    editor,
    name: "listItem",
    options: listItemExt.options,
    type: editor.schema.nodes.listItem,
    storage: listItemExt.storage,
  } as never)();
  const enter = shortcuts?.Enter;
  if (!enter) throw new Error("Enter shortcut not bound");
  return enter();
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
