/**
 * ProseMirror plugin that paints the other editors' carets and selections
 * inside the note (FIR-1317).
 *
 * It is registered on the shared ContentEditor through `registerPlugin`, so
 * live editing needs no change to the upstream editor component.
 */

import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type { Node as PMNode } from "@tiptap/pm/model";

import { clampCaret, type RemoteCaret } from "./note-live-collab-protocol";

export const remoteCaretsKey = new PluginKey<DecorationSet>(
  "cerebroNoteRemoteCarets",
);

function caretWidget(caret: RemoteCaret): HTMLElement {
  const wrapper = document.createElement("span");
  wrapper.className = "cerebro-remote-caret";
  wrapper.style.position = "relative";
  wrapper.style.display = "inline-block";
  wrapper.style.width = "0";

  const bar = document.createElement("span");
  bar.style.position = "absolute";
  bar.style.top = "0";
  bar.style.bottom = "0";
  bar.style.width = "2px";
  bar.style.backgroundColor = caret.color;
  bar.style.pointerEvents = "none";

  const label = document.createElement("span");
  label.textContent = caret.name || "Someone";
  label.style.position = "absolute";
  label.style.top = "-1.25rem";
  label.style.left = "0";
  label.style.whiteSpace = "nowrap";
  label.style.borderRadius = "3px";
  label.style.padding = "0 4px";
  label.style.fontSize = "10px";
  label.style.lineHeight = "16px";
  label.style.color = "#fff";
  label.style.backgroundColor = caret.color;
  label.style.pointerEvents = "none";

  bar.appendChild(label);
  wrapper.appendChild(bar);
  return wrapper;
}

/**
 * buildCaretDecorations turns the known remote carets into decorations against
 * the local document. Positions are clamped because a caret is produced
 * against the sender's document, which can be one step ahead of ours.
 */
export function buildCaretDecorations(
  doc: PMNode,
  carets: RemoteCaret[],
): DecorationSet {
  const size = doc.content.size;
  const decorations: Decoration[] = [];
  for (const caret of carets) {
    const head = clampCaret(caret.head, size);
    const anchor = clampCaret(caret.anchor, size);
    const from = Math.min(anchor, head);
    const to = Math.max(anchor, head);
    if (to > from) {
      decorations.push(
        Decoration.inline(from, to, {
          style: `background-color: ${caret.color}33;`,
        }),
      );
    }
    decorations.push(
      Decoration.widget(head, () => caretWidget(caret), {
        side: 1,
        key: `caret-${caret.peerId}-${head}`,
      }),
    );
  }
  return DecorationSet.create(doc, decorations);
}

/**
 * remoteCaretsPlugin holds the decoration set. New carets arrive as plugin
 * metadata on an otherwise empty transaction; between those, existing
 * decorations are mapped through document changes so a caret keeps pointing at
 * the same text while the local user types above it.
 */
export function remoteCaretsPlugin(): Plugin<DecorationSet> {
  return new Plugin<DecorationSet>({
    key: remoteCaretsKey,
    state: {
      init: () => DecorationSet.empty,
      apply(tr: Transaction, value: DecorationSet) {
        const carets = tr.getMeta(remoteCaretsKey) as RemoteCaret[] | undefined;
        if (carets) return buildCaretDecorations(tr.doc, carets);
        return value.map(tr.mapping, tr.doc);
      },
    },
    props: {
      decorations(state: EditorState) {
        return remoteCaretsKey.getState(state) ?? DecorationSet.empty;
      },
    },
  });
}
