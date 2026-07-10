import { describe, expect, it } from "vitest";
import { Schema } from "@tiptap/pm/model";
import type { Node as PMNode } from "@tiptap/pm/model";
import { EditorState, TextSelection } from "@tiptap/pm/state";
import { authorCodePlugin, lineStampOffset } from "./author-code-plugin";

const schema = new Schema({
  nodes: {
    doc: { content: "block+" },
    paragraph: { group: "block", content: "inline*" },
    heading: { group: "block", content: "inline*" },
    codeBlock: { group: "block", content: "inline*" },
    bulletList: { group: "block", content: "listItem+" },
    listItem: { content: "paragraph+" },
    text: { group: "inline" },
  },
  marks: { bold: {} },
});

const bold = schema.marks.bold!.create();

function p(...content: PMNode[]): PMNode {
  return schema.node("paragraph", null, content);
}

describe("lineStampOffset", () => {
  it("stamps a plain paragraph at its start", () => {
    expect(lineStampOffset(p(schema.text("hello there")), "JEH")).toBe(0);
  });

  it("never stamps headings or code blocks", () => {
    expect(
      lineStampOffset(schema.node("heading", null, [schema.text("IDS")]), "JEH"),
    ).toBe(null);
    expect(
      lineStampOffset(
        schema.node("codeBlock", null, [schema.text("select 1")]),
        "JEH",
      ),
    ).toBe(null);
  });

  it("skips empty blocks", () => {
    expect(lineStampOffset(p(), "JEH")).toBe(null);
    expect(lineStampOffset(p(schema.text("   ")), "JEH")).toBe(null);
  });

  it("skips lines already carrying the writer's own code", () => {
    expect(lineStampOffset(p(schema.text("JEH already stamped")), "JEH")).toBe(
      null,
    );
    expect(lineStampOffset(p(schema.text("JEH")), "JEH")).toBe(null);
  });

  it("skips lines whose first text is an existing bold stamp", () => {
    expect(
      lineStampOffset(
        p(schema.text("MOP", [bold]), schema.text(" wrote this")),
        "JEH",
      ),
    ).toBe(null);
  });

  it("still stamps plain lines starting with acronyms", () => {
    expect(
      lineStampOffset(p(schema.text("AI agent vi kan sætte igang")), "JEH"),
    ).toBe(0);
    expect(lineStampOffset(p(schema.text("KPI i month view")), "JEH")).toBe(0);
  });

  it("stamps after a literal list marker — '- **JEH** teksten'", () => {
    expect(lineStampOffset(p(schema.text("- punkt et")), "JEH")).toBe(2);
    expect(lineStampOffset(p(schema.text("* punkt to")), "JEH")).toBe(2);
  });

  it("leaves a bare list marker alone (line not written yet)", () => {
    expect(lineStampOffset(p(schema.text("- ")), "JEH")).toBe(null);
  });

  it("skips a literal-marker line that is already stamped", () => {
    expect(
      lineStampOffset(
        p(
          schema.text("- "),
          schema.text("JEH", [bold]),
          schema.text(" teksten"),
        ),
        "JEH",
      ),
    ).toBe(null);
    expect(
      lineStampOffset(
        p(
          schema.text("- "),
          schema.text("MOP", [bold]),
          schema.text(" teksten"),
        ),
        "JEH",
      ),
    ).toBe(null);
  });

  it("stamps a real bullet paragraph at its content start", () => {
    // listItem > paragraph: the "-" marker lives outside the paragraph, so the
    // markdown serialization becomes "- **JEH** teksten".
    expect(lineStampOffset(p(schema.text("teksten")), "JEH")).toBe(0);
  });
});

// --- plugin timing: the stamp fires when the line is FINISHED (cursor leaves
// the block), never while the user is still typing on it. ---

function makeState(enabled = true, code = "JEH") {
  const doc = schema.node("doc", null, [
    p(),
    p(schema.text("existing line")),
  ]);
  return EditorState.create({
    doc,
    selection: TextSelection.create(doc, 1), // inside the empty first paragraph
    plugins: [authorCodePlugin(() => ({ enabled, code }))],
  });
}

describe("authorCodePlugin", () => {
  it("does not stamp while the user is typing on the line", () => {
    let state = makeState();
    state = state.apply(state.tr.insertText("hej"));
    expect(state.doc.firstChild!.textContent).toBe("hej");
  });

  it("stamps in bold when the cursor leaves the line", () => {
    let state = makeState();
    state = state.apply(state.tr.insertText("hej"));
    // Move the cursor into the second paragraph — the line is finished.
    const secondStart = state.doc.firstChild!.nodeSize + 1;
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, secondStart)),
    );
    const first = state.doc.firstChild!;
    expect(first.textContent).toBe("JEH hej");
    expect(first.firstChild!.text).toBe("JEH");
    expect(first.firstChild!.marks.some((m) => m.type.name === "bold")).toBe(
      true,
    );
    expect(first.child(1).text).toBe(" hej");
    expect(first.child(1).marks.length).toBe(0);
  });

  it("stamps when Enter splits off a new line", () => {
    let state = makeState();
    state = state.apply(state.tr.insertText("hej"));
    state = state.apply(state.tr.split(state.selection.from));
    expect(state.doc.firstChild!.textContent).toBe("JEH hej");
  });

  it("puts the code after a literal list marker", () => {
    let state = makeState();
    state = state.apply(state.tr.insertText("- punkt"));
    const secondStart = state.doc.firstChild!.nodeSize + 1;
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, secondStart)),
    );
    const first = state.doc.firstChild!;
    expect(first.textContent).toBe("- JEH punkt");
    expect(first.child(1).text).toBe("JEH");
    expect(first.child(1).marks.some((m) => m.type.name === "bold")).toBe(true);
  });

  it("never stamps a pre-existing line the user edits", () => {
    let state = makeState();
    // Jump into the second (already non-empty) paragraph and append text.
    const secondStart = state.doc.firstChild!.nodeSize + 1;
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, secondStart)),
    );
    state = state.apply(state.tr.insertText("mere "));
    // Leave the line again.
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, 1)),
    );
    expect(state.doc.child(1).textContent).toBe("mere existing line");
  });

  it("does not stamp a line emptied before leaving it", () => {
    let state = makeState();
    state = state.apply(state.tr.insertText("hej"));
    state = state.apply(state.tr.delete(1, 4));
    const secondStart = state.doc.firstChild!.nodeSize + 1;
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, secondStart)),
    );
    expect(state.doc.firstChild!.textContent).toBe("");
  });

  it("does nothing when disabled", () => {
    let state = makeState(false);
    state = state.apply(state.tr.insertText("hej"));
    const secondStart = state.doc.firstChild!.nodeSize + 1;
    state = state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, secondStart)),
    );
    expect(state.doc.firstChild!.textContent).toBe("hej");
  });
});
