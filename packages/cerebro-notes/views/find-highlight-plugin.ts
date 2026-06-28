// Find-highlight decoration plugin for the inline Find & Replace bar (FIR-2145).
//
// Paints every occurrence of the search query inside the TipTap editor as a
// yellow background; the "active" match (the one being navigated to) gets a
// brighter tint plus an outline. Registered at runtime via
// editor.registerPlugin() from useFindHighlight so the upstream ContentEditor
// is never modified. Modelled after comment-anchor-plugin.ts.

import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Node as PMNode } from "@tiptap/pm/model";

export interface FindHighlightState {
  query: string;
  activeIndex: number;
}

export const findHighlightKey = new PluginKey<FindHighlightState>(
  "findHighlight",
);

const BASE_STYLE =
  "background-color:rgba(234,179,8,0.28);border-radius:2px;box-decoration-break:clone;-webkit-box-decoration-break:clone;";
const ACTIVE_STYLE =
  "background-color:rgba(234,179,8,0.65);border-radius:2px;box-decoration-break:clone;-webkit-box-decoration-break:clone;outline:1.5px solid rgba(180,135,0,0.7);";

// collectText flattens the ProseMirror doc to plain text, tracking the
// doc-position of each character. Mirrors comment-anchor-plugin.ts so the
// search aligns with what the user reads in the editor, not the raw markdown.
function collectText(doc: PMNode): { text: string; map: number[] } {
  let text = "";
  const map: number[] = [];
  let needBreak = false;
  doc.descendants((node, pos) => {
    if (node.isText && node.text) {
      if (needBreak && text.length > 0) {
        text += "\n";
        map.push(pos);
        needBreak = false;
      }
      for (let i = 0; i < node.text.length; i++) {
        text += node.text[i];
        map.push(pos + i);
      }
    } else if (node.isBlock && text.length > 0) {
      needBreak = true;
    }
    return true;
  });
  return { text, map };
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function buildDecorations(
  doc: PMNode,
  state: FindHighlightState,
): DecorationSet {
  const { query, activeIndex } = state;
  if (!query) return DecorationSet.empty;
  const { text, map } = collectText(doc);
  const re = new RegExp(escapeRegExp(query), "gi");
  const decos: Decoration[] = [];
  let match: RegExpExecArray | null;
  let idx = 0;
  while ((match = re.exec(text)) !== null) {
    const start = match.index;
    const end = start + match[0].length - 1;
    const from = map[start];
    const to = map[end];
    if (from === undefined || to === undefined) {
      idx++;
      continue;
    }
    const isActive = idx === activeIndex;
    decos.push(
      Decoration.inline(from, to + 1, {
        style: isActive ? ACTIVE_STYLE : BASE_STYLE,
        class: isActive
          ? "find-highlight find-highlight-active"
          : "find-highlight",
        "data-find-index": String(idx),
      }),
    );
    idx++;
  }
  return DecorationSet.create(doc, decos);
}

export function findHighlightPlugin(): Plugin<FindHighlightState> {
  return new Plugin<FindHighlightState>({
    key: findHighlightKey,
    state: {
      init: () => ({ query: "", activeIndex: -1 }),
      apply(tr, value) {
        const meta = tr.getMeta(
          findHighlightKey,
        ) as FindHighlightState | undefined;
        return meta ?? value;
      },
    },
    props: {
      decorations(editorState) {
        const s = findHighlightKey.getState(editorState);
        if (!s || !s.query) return null;
        return buildDecorations(editorState.doc, s);
      },
    },
  });
}
