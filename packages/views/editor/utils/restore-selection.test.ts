import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { TextSelection } from "@tiptap/pm/state";
import { clampSelectionToText } from "./restore-selection";

interface JsonNode {
  type: string;
  text?: string;
  content?: JsonNode[];
}

let editor: Editor | undefined;

function makeEditor(content: JsonNode): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({ element, extensions: [StarterKit], content });
  return editor;
}

afterEach(() => {
  editor?.destroy();
  editor = undefined;
  document.body.innerHTML = "";
});

/** doc > orderedList > listItem > paragraph("todo") */
const orderedListDoc: JsonNode = {
  type: "doc",
  content: [
    {
      type: "orderedList",
      content: [
        {
          type: "listItem",
          content: [{ type: "paragraph", content: [{ type: "text", text: "todo" }] }],
        },
      ],
    },
  ],
};

describe("clampSelectionToText", () => {
  it("snaps a structural offset onto real list text instead of the pre-item gap (ordered-list cursor bug)", () => {
    const ed = makeEditor(orderedListDoc);

    // Offset 1 is the gap between <orderedList> and its first <listItem> — a
    // non-text boundary. The old `setTextSelection({ from: Math.min(1, size) })`
    // parked the caret here, which the browser rendered on the next line.
    const sel = clampSelectionToText(ed.state.doc, 1, 1);

    expect(sel instanceof TextSelection).toBe(true);
    expect(sel.empty).toBe(true);
    // Snapped inside the list item's paragraph — a placeable text cursor.
    expect(sel.$from.parent.type.name).toBe("paragraph");
    expect(sel.$from.parent.isTextblock).toBe(true);
    // "todo" text spans positions 3..7; the snap lands at its start (3).
    expect(sel.from).toBe(3);
  });

  it("preserves an already-valid caret position unchanged", () => {
    const ed = makeEditor({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "hello" }] }],
    });

    // Position 3 sits between "he" and "llo" — a real text position.
    const sel = clampSelectionToText(ed.state.doc, 3, 3);

    expect(sel.from).toBe(3);
    expect(sel.$from.parent.isTextblock).toBe(true);
  });

  it("clamps an out-of-range offset to a valid text position at the document end", () => {
    const ed = makeEditor(orderedListDoc);

    const sel = clampSelectionToText(ed.state.doc, 999, 999);

    expect(sel.$from.parent.isTextblock).toBe(true);
    // End of the only text ("todo").
    expect(sel.from).toBe(7);
  });
});
