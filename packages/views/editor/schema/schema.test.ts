import { afterEach, describe, expect, it } from "vitest";
import { Editor, getSchema } from "@tiptap/core";
import type { Schema } from "@tiptap/pm/model";
import * as Y from "yjs";
import { createEditorExtensions } from "../extensions";
import { createMarkdownSchemaExtensions } from ".";
import { COLLAB_FIELD, createDocumentCodec } from "./codec";

const editorExtensions = () =>
  createEditorExtensions({
    placeholder: "",
    enableSlashCommands: false,
    onUploadFileRef: { current: undefined },
  });

function describeSchema(schema: Schema) {
  const nodes = Object.fromEntries(
    Object.entries(schema.nodes).map(([name, type]) => [
      name,
      { attrs: Object.keys(type.spec.attrs ?? {}).sort(), group: type.spec.group ?? null, content: type.spec.content ?? null },
    ]),
  );
  const marks = Object.fromEntries(
    Object.entries(schema.marks).map(([name, type]) => [
      name,
      { attrs: Object.keys(type.spec.attrs ?? {}).sort(), inclusive: type.spec.inclusive ?? null },
    ]),
  );
  return { nodes, marks };
}

let editor: Editor | null = null;
afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("schema-only extensions", () => {
  // The collaboration server builds documents from this list. A node or mark
  // the editor knows but the server does not would be dropped from every
  // collaborative document the server touches.
  it("describe the same schema as the editor", () => {
    expect(describeSchema(getSchema(createMarkdownSchemaExtensions()))).toEqual(
      describeSchema(getSchema(editorExtensions())),
    );
  });

  it("serialize Markdown the way the editor does", () => {
    const markdown = [
      "# Title",
      "",
      "Some **bold** and ==highlight== with [@Ada](mention://member/u1) and [MUL-1](mention://issue/i1).",
      "",
      "- [ ] task",
      "- [x] done",
      "",
      "1. one",
      "   1. nested",
      "",
      "```ts title=\"x\"",
      "const a = 1;",
      "```",
      "",
      "$$",
      "e=mc^2",
      "$$",
      "",
      "| a | b |",
      "| --- | --- |",
      "| 1 | 2 |",
      "",
      "![alt](https://example.com/a.png)",
      "",
      "!file[report.pdf](https://example.com/report.pdf)",
    ].join("\n");
    const element = document.createElement("div");
    document.body.appendChild(element);
    editor = new Editor({ element, extensions: editorExtensions() });
    const codec = createDocumentCodec();
    editor.commands.setContent(codec.parse(markdown));
    expect(codec.serialize(codec.parse(markdown))).toBe(editor.getMarkdown().trim());
  });
});

describe("document codec", () => {
  const codec = createDocumentCodec();

  it("round-trips Markdown through a Y.Doc", () => {
    const markdown = "# Plan\n\nFirst paragraph.\n\n- a\n- b";
    expect(codec.ydocToMarkdown(codec.createYDoc(markdown))).toBe(markdown);
  });

  it("keeps the editor's trailing paragraph after a final list, through rewrites", () => {
    const ydoc = codec.createYDoc("Intro\n\n- a");
    const last = () => ydoc.getXmlFragment(COLLAB_FIELD).toArray().at(-1) as Y.XmlElement;
    expect(last().nodeName).toBe("paragraph");
    expect(codec.ydocToMarkdown(ydoc)).toBe("Intro\n\n- a");
    codec.applyJSON(ydoc, codec.parse("Intro\n\n- a\n- b"));
    expect(last().nodeName).toBe("paragraph");
    expect(ydoc.getXmlFragment(COLLAB_FIELD).length).toBe(3);
    expect(codec.ydocToMarkdown(ydoc)).toBe("Intro\n\n- a\n- b");
  });

  it.each([
    ["ordered", "8. **Keep**:\n   - a\n   - b\n\n   **Drop**:\n   - c\n\n   Closing note."],
    ["bullet", "- a\n- b:\n  - x\n  - y\n\n  [tag]"],
  ])("keeps a %s item's paragraph after a nested list stable across saves", (_kind, markdown) => {
    const saved = codec.serialize(codec.parse(markdown));
    expect(codec.serialize(codec.parse(saved))).toBe(saved);
    expect(saved).not.toMatch(/^ {4,}\S/m);
    expect(JSON.stringify(codec.parse(markdown))).not.toMatch(/"text":" /);
  });

  it("keeps an empty document editable", () => {
    const ydoc = codec.createYDoc("");
    expect(ydoc.getXmlFragment(COLLAB_FIELD).length).toBe(1);
    expect(codec.ydocToMarkdown(ydoc)).toBe("");
  });

  it("applies new Markdown as a diff that keeps untouched blocks", () => {
    const ydoc = codec.createYDoc("Alpha\n\nBeta\n\nGamma");
    const fragment = ydoc.getXmlFragment(COLLAB_FIELD);
    const [alpha, , gamma] = fragment.toArray();
    codec.applyJSON(ydoc, codec.parse("Alpha\n\nBeta edited\n\nGamma"), "agent");
    expect(codec.ydocToMarkdown(ydoc)).toBe("Alpha\n\nBeta edited\n\nGamma");
    expect(fragment.get(0)).toBe(alpha);
    expect(fragment.get(2)).toBe(gamma);
  });

  it("merges a server-side rewrite with a concurrent client edit", () => {
    const server = codec.createYDoc("Alpha\n\nBeta\n\nGamma");
    const client = new Y.Doc();
    Y.applyUpdate(client, Y.encodeStateAsUpdate(server));

    // The client types into Gamma while the server rewrites Alpha.
    const gammaText = client.getXmlFragment(COLLAB_FIELD).get(2) as Y.XmlElement;
    (gammaText.get(0) as Y.XmlText).insert(5, " ray");
    codec.applyJSON(server, codec.parse("Alpha changed\n\nBeta\n\nGamma"), "agent");

    Y.applyUpdate(server, Y.encodeStateAsUpdate(client));
    Y.applyUpdate(client, Y.encodeStateAsUpdate(server));
    expect(codec.ydocToMarkdown(server)).toBe("Alpha changed\n\nBeta\n\nGamma ray");
    expect(codec.ydocToMarkdown(client)).toBe(codec.ydocToMarkdown(server));
  });
});
