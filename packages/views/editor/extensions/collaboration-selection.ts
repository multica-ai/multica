import { Extension } from "@tiptap/core";
import { Plugin, TextSelection } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type * as Y from "yjs";

/**
 * Keeps a click or an arrow key from being undone by a collaborator's change.
 *
 * ProseMirror reads a selection the browser made on the next `selectionchange`
 * event. A collaborator's change applied before that makes the sync plugin
 * save the old selection and put it back, so with people typing a click could
 * leave the caret at the top of the doc. Before a remote change applies, this
 * reads the browser's selection into the editor so the sync plugin keeps it.
 * It must run before the Collaboration extension's own handler, hence the
 * higher priority.
 */
export const CollaborationSelectionSync = Extension.create<{ document: Y.Doc | null }>({
  name: "collaborationSelectionSync",
  priority: 1001,

  addOptions() {
    return { document: null };
  },

  addProseMirrorPlugins() {
    const ydoc = this.options.document;
    if (!ydoc) return [];
    return [
      new Plugin({
        view(view) {
          const sync = () => {
            if ((ydoc as unknown as { _transaction: Y.Transaction | null })._transaction?.local !== false) return;
            readDOMSelection(view);
          };
          ydoc.on("beforeAllTransactions", sync);
          return { destroy: () => ydoc.off("beforeAllTransactions", sync) };
        },
      }),
    ];
  },
});

function readDOMSelection(view: EditorView) {
  if (view.composing || !view.hasFocus() || !(view.state.selection instanceof TextSelection)) return;
  const selection = view.dom.ownerDocument.getSelection();
  if (!selection?.anchorNode || !selection.focusNode || !view.dom.contains(selection.anchorNode)) return;
  let anchor: number;
  let head: number;
  try {
    anchor = view.posAtDOM(selection.anchorNode, selection.anchorOffset);
    head = view.posAtDOM(selection.focusNode, selection.focusOffset);
  } catch {
    return;
  }
  const { doc } = view.state;
  const next = TextSelection.between(doc.resolve(anchor), doc.resolve(head));
  if (next.eq(view.state.selection)) return;
  view.dispatch(view.state.tr.setSelection(next));
}
