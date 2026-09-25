import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import type { Node as PMNode } from "@tiptap/pm/model";
import { createEditorExtensions } from "../extensions";
import { repairEmptyListItems } from "./repair-list-items";

// A REAL editor — real createEditorExtensions + real @tiptap/markdown, no
// @tiptap/react mock. This is the layer the reverted #4813 failed to exercise:
// its green suite validated a hand-fabricated `editorState.markdown`, not the
// actual markdown round-trip.
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

function firstItem(ed: Editor, name = "listItem"): PMNode {
  let found: PMNode | undefined;
  ed.state.doc.descendants((node) => {
    if (!found && node.type.name === name) found = node;
    return true;
  });
  if (!found) throw new Error(`no ${name} found`);
  return found;
}

afterEach(() => {
  editor?.destroy();
  editor = undefined;
  document.body.innerHTML = "";
});

describe("repairEmptyListItems (real editor)", () => {
  it("reproduces the corrupt empty-ordered-list round-trip, then repairs it", () => {
    // The draft persisted after typing `1.` in a comment, restored on remount.
    const ed = makeEditor("1. \n\n");

    // Failing-first: @tiptap/markdown parses the empty item into a childless
    // listItem, and the document is left with no real text cursor.
    expect(firstItem(ed).childCount).toBe(0);
    const before = ed.state.selection;
    expect(before instanceof TextSelection && before.$cursor != null).toBe(false);

    repairEmptyListItems(ed);

    // The list item now holds a paragraph and the caret sits inside it (line 1),
    // ready to keep typing — not on the following block.
    const li = firstItem(ed);
    expect(li.childCount).toBe(1);
    expect(li.firstChild?.type.name).toBe("paragraph");
    const after = ed.state.selection;
    expect(after instanceof TextSelection).toBe(true);
    expect((after as TextSelection).$cursor).not.toBeNull();
    expect(after.$from.parent.type.name).toBe("paragraph");
    expect(after.$from.node(-1)?.type.name).toBe("listItem");
    // The repaired document round-trips cleanly.
    expect(ed.getMarkdown()).toBe("1. \n\n");
  });

  it("does not leave the repair on the undo stack (Ctrl-Z must not revive the bug)", () => {
    const ed = makeEditor("1. \n\n");
    repairEmptyListItems(ed);
    expect(firstItem(ed).childCount).toBe(1);

    // Undo must not restore the childless item / AllSelection.
    expect(ed.can().undo()).toBe(false);
    ed.commands.undo();
    expect(firstItem(ed).childCount).toBe(1);
    expect(ed.state.selection instanceof TextSelection).toBe(true);
  });

  it("keeps the document schema-valid across undo/redo when a prior edit history exists", () => {
    const ed = makeEditor("hello\n\n");
    // Build real undo history, then simulate an external empty-list update.
    ed.commands.insertContentAt(ed.state.doc.content.size - 1, " world");
    ed.commands.setContent("1. \n\n", { contentType: "markdown" });
    repairEmptyListItems(ed);

    for (let i = 0; i < 4; i++) {
      ed.commands.undo();
      expect(() => ed.state.doc.check()).not.toThrow();
    }
    for (let i = 0; i < 4; i++) {
      ed.commands.redo();
      expect(() => ed.state.doc.check()).not.toThrow();
    }
  });

  it("repairs a nested empty ordered list to a schema-valid document", () => {
    // The outer item's only child is the sub-list (not a paragraph), so it is
    // itself schema-invalid until we prepend a paragraph.
    const ed = makeEditor("1. \n   1. \n\n");
    expect(() => ed.state.doc.check()).toThrow();

    repairEmptyListItems(ed);

    // No schema violation remains, and there is a real text cursor.
    expect(() => ed.state.doc.check()).not.toThrow();
    expect(ed.state.selection instanceof TextSelection).toBe(true);
    expect((ed.state.selection as TextSelection).$cursor).not.toBeNull();
  });

  it("repairs a directly-constructed malformed list (bullet item with no paragraph)", () => {
    // Guards the general contract beyond the ordered-list markdown case.
    const ed = makeEditor();
    ed.view.dispatch(
      ed.state.tr.setSelection(TextSelection.near(ed.state.doc.resolve(0))),
    );
    ed.commands.setContent(
      {
        type: "doc",
        content: [{ type: "bulletList", content: [{ type: "listItem" }] }],
      },
      { emitUpdate: false },
    );
    expect(firstItem(ed).childCount).toBe(0);

    repairEmptyListItems(ed);

    expect(firstItem(ed).childCount).toBe(1);
    expect(firstItem(ed).firstChild?.type.name).toBe("paragraph");
    expect(ed.state.selection instanceof TextSelection).toBe(true);
    expect(ed.state.selection.$from.node(-1)?.type.name).toBe("listItem");
  });

  it("is a no-op for a non-empty list (round-trips fine) — caret untouched", () => {
    const ed = makeEditor("1. buy milk\n\n");
    const pos = 4; // mid-word
    ed.view.dispatch(
      ed.state.tr.setSelection(TextSelection.create(ed.state.doc, pos)),
    );
    const json = JSON.stringify(ed.getJSON());

    repairEmptyListItems(ed);

    expect(ed.state.selection.from).toBe(pos);
    expect(JSON.stringify(ed.getJSON())).toBe(json);
  });

  it("is a no-op for plain paragraph content", () => {
    const ed = makeEditor("hello world\n\n");
    const json = JSON.stringify(ed.getJSON());
    repairEmptyListItems(ed);
    expect(JSON.stringify(ed.getJSON())).toBe(json);
  });

  it("works from the onCreate mount hook (the path ContentEditor uses)", async () => {
    const el = document.createElement("div");
    document.body.appendChild(el);
    editor = new Editor({
      element: el,
      extensions: createEditorExtensions({}),
      content: "1. \n\n",
      contentType: "markdown",
      onCreate: ({ editor: ed }) => repairEmptyListItems(ed),
    });
    // Tiptap defers onCreate a tick past construction.
    await new Promise((r) => setTimeout(r, 0));

    const li = firstItem(editor);
    expect(li.childCount).toBe(1);
    expect(li.firstChild?.type.name).toBe("paragraph");
    expect(editor.state.selection instanceof TextSelection).toBe(true);
    expect(editor.state.selection.$from.node(-1)?.type.name).toBe("listItem");
  });
});
