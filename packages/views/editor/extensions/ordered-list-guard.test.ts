import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import type { JSONContent } from "@tiptap/core";
import { createEditorExtensions } from ".";

function makeEditor(content: JSONContent) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: createEditorExtensions({
      onUploadFileRef: { current: undefined },
    }),
    content,
  });
}

function applyTextInput(editor: Editor, text: string): boolean {
  const { from, to } = editor.state.selection;
  const handled =
    editor.view.someProp("handleTextInput", (handler) =>
      handler(editor.view, from, to, text, () => editor.state.tr),
    ) === true;

  if (!handled) {
    editor.view.dispatch(editor.state.tr.insertText(text, from, to));
  }

  return handled;
}

function setSelectionInFirstEmptyParagraph(editor: Editor) {
  let selectionPos: number | null = null;

  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === "paragraph" && node.content.size === 0) {
      selectionPos = pos + 1;
      return false;
    }
    return undefined;
  });

  expect(selectionPos).not.toBeNull();
  editor.commands.setTextSelection(selectionPos!);
}

function containsNestedOrderedList(node: JSONContent): boolean {
  if (node.type === "listItem") {
    return (node.content ?? []).some((child) => child.type === "orderedList");
  }

  return (node.content ?? []).some(containsNestedOrderedList);
}

describe("orderedListInputGuard", () => {
  let editor: Editor | null = null;

  afterEach(() => {
    editor?.destroy();
    editor = null;
    document.body.innerHTML = "";
  });

  it("keeps typed numbering literal inside an existing list item", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "orderedList",
          attrs: { start: 1 },
          content: [
            {
              type: "listItem",
              content: [
                {
                  type: "paragraph",
                  content: [{ type: "text", text: "First item" }],
                },
              ],
            },
            {
              type: "listItem",
              content: [{ type: "paragraph" }],
            },
          ],
        },
      ],
    });

    setSelectionInFirstEmptyParagraph(editor);
    expect(applyTextInput(editor, "2. ")).toBe(true);

    const json = editor.getJSON() as JSONContent;
    const orderedList = json.content?.[0];
    expect(orderedList?.type).toBe("orderedList");
    expect(orderedList?.content).toHaveLength(2);
    expect(containsNestedOrderedList(json)).toBe(false);
    expect(orderedList?.content?.[1]?.content?.[0]?.content?.[0]).toMatchObject({
      type: "text",
      text: "2. ",
    });
  });
});
