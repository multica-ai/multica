import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";

// Covers patches/@tiptap__y-tiptap@3.3.1.patch: drop the patch only once an
// upgraded @tiptap/y-tiptap passes this on its own.

let editor: Editor | null = null;
afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

function setup() {
  const shared = new Y.Doc();
  const fragment = shared.getXmlFragment("default");
  const paragraph = (text: string) => {
    const p = new Y.XmlElement("paragraph");
    p.insert(0, [new Y.XmlText(text)]);
    return p;
  };
  fragment.insert(0, [paragraph("First"), paragraph("Second"), paragraph("Third")]);
  const local = new Y.Doc();
  Y.applyUpdate(local, Y.encodeStateAsUpdate(shared));
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: [StarterKit.configure({ undoRedo: false }), Collaboration.configure({ document: local })],
  });
  return { editor, local };
}

describe("caret restore after a collaborator's change", () => {
  it("keeps the caret in its own new paragraph when a collaborator opens one above it", () => {
    const { editor, local } = setup();
    // Enter at the end of "Third": the caret sits in a fresh empty paragraph.
    editor.commands.focus("end");
    editor.commands.splitBlock();
    expect(editor.state.selection.$from.index(0)).toBe(3);

    // Meanwhile a collaborator presses Enter at the end of "First".
    const remote = new Y.Doc();
    Y.applyUpdate(remote, Y.encodeStateAsUpdate(local));
    const before = Y.encodeStateVector(remote);
    remote.getXmlFragment("default").insert(1, [new Y.XmlElement("paragraph")]);
    Y.applyUpdate(local, Y.encodeStateAsUpdate(remote, before), "remote");

    expect(editor.state.doc.childCount).toBe(5);
    expect(editor.state.selection.$from.index(0)).toBe(4);
    editor.commands.insertContent("mine");
    expect(editor.state.doc.child(1).textContent).toBe("");
    expect(editor.state.doc.child(4).textContent).toBe("mine");
  });
});
