import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";
import { CollaborationSelectionSync } from "./collaboration-selection";

let editor: Editor | null = null;
afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

function setup(withSync: boolean) {
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
    extensions: [
      StarterKit.configure({ undoRedo: false }),
      Collaboration.configure({ document: local }),
      ...(withSync ? [CollaborationSelectionSync.configure({ document: local })] : []),
    ],
  });
  const view = editor.view;
  view.focus();
  // A click into "Second" that ProseMirror has not read yet: the browser has
  // moved the selection, the selectionchange event is still queued.
  const second = view.dom.querySelectorAll("p")[1]!.firstChild!;
  window.getSelection()!.collapse(second, 3);
  // A collaborator types in "Third" before that event runs.
  const remote = new Y.Doc();
  Y.applyUpdate(remote, Y.encodeStateAsUpdate(local));
  const before = Y.encodeStateVector(remote);
  ((remote.getXmlFragment("default").get(2) as Y.XmlElement).get(0) as Y.XmlText).insert(5, "!");
  Y.applyUpdate(local, Y.encodeStateAsUpdate(remote, before), "remote");
  return view;
}

describe("CollaborationSelectionSync", () => {
  it("keeps a selection the editor has not read yet when a collaborator's change arrives", () => {
    const view = setup(true);
    expect(view.state.doc.child(2).textContent).toBe("Third!");
    expect(view.state.selection.$from.parent.textContent).toBe("Second");
    expect(view.state.selection.$from.parentOffset).toBe(3);
  });

  it("without it, the sync plugin puts the old selection back", () => {
    const view = setup(false);
    expect(view.state.selection.$from.parent.textContent).toBe("First");
  });
});
