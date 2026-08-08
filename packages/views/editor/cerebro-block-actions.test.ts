import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { PatchedListItem } from "./extensions/list-item";
import { CerebroBlockActions } from "./cerebro-block-actions";

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
      CerebroBlockActions,
    ],
    content,
  });
}

/** Inside-paragraph position of the i-th listItem. */
function listItemTextPos(editor: Editor, index: number): number {
  let count = 0;
  let pos = -1;
  editor.state.doc.descendants((node, p) => {
    if (pos >= 0) return false;
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
      content: [
        { type: "paragraph", content: text ? [{ type: "text", text }] : [] },
      ],
    })),
  };
}

function topItemTexts(editor: Editor): string[] {
  const json = editor.getJSON() as JsonNode;
  const list = json.content?.find((n) => n.type === "bulletList");
  return (list?.content ?? []).map(
    (li) => li.content?.[0]?.content?.[0]?.text ?? "",
  );
}

describe("CerebroBlockActions — duplicate", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("inserts a copy of the list item right after it", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0)); // in "a"
    expect(editor.commands.duplicateBlock()).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "a", "b"]);
  });

  it("carries the item's nested children into the copy", () => {
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
                bullet("a1"),
              ],
            },
          ],
        },
      ],
    });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    expect(editor.commands.duplicateBlock()).toBe(true);
    const list = (editor.getJSON() as JsonNode).content?.[0];
    expect(list?.content).toHaveLength(2);
    // The copy keeps the nested "a1".
    expect(list?.content?.[1]?.content?.[1]?.type).toBe("bulletList");
  });

  it("duplicates a top-level paragraph outside any list", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "hi" }] }],
    });
    editor.commands.setTextSelection(1);
    expect(editor.commands.duplicateBlock()).toBe(true);
    const paras = (editor.getJSON() as JsonNode).content?.filter(
      (n) => n.type === "paragraph" && n.content?.[0]?.text === "hi",
    );
    expect(paras).toHaveLength(2);
  });
});

describe("CerebroBlockActions — delete", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("removes the single targeted item", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b", "c")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1)); // in "b"
    expect(editor.commands.deleteBlock()).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "c"]);
  });

  it("removes every item a selection spans", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b", "c", "d")] });
    // Selection from inside "b" to inside "c".
    editor.commands.setTextSelection({
      from: listItemTextPos(editor, 1),
      to: listItemTextPos(editor, 2),
    });
    expect(editor.commands.deleteBlock()).toBe(true);
    expect(topItemTexts(editor)).toEqual(["a", "d"]);
  });
});

describe("CerebroBlockActions — move", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("reorders a list item down past its sibling (drag reorder)", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b", "c")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 0));
    expect(editor.commands.moveBlock(1)).toBe(true);
    expect(topItemTexts(editor)).toEqual(["b", "a", "c"]);
  });

  it("reorders a top-level paragraph up past its predecessor", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "one" }] },
        { type: "paragraph", content: [{ type: "text", text: "two" }] },
      ],
    });
    editor.commands.setTextSelection(6); // inside "two"
    expect(editor.commands.moveBlock(-1)).toBe(true);
    const texts = (editor.getJSON() as JsonNode).content?.map(
      (n) => n.content?.[0]?.text,
    );
    expect(texts).toEqual(["two", "one"]);
  });

  it("is a no-op at the bottom edge", () => {
    editor = makeEditor({ type: "doc", content: [bullet("a", "b")] });
    editor.commands.setTextSelection(listItemTextPos(editor, 1));
    expect(editor.commands.moveBlock(1)).toBe(false);
    expect(topItemTexts(editor)).toEqual(["a", "b"]);
  });
});
