// Comment-anchor highlight plugin (TECH-3637).
//
// A note comment / suggestion can be anchored to a quoted span of the body.
// The anchor is stored as the quoted TEXT (plus prefix/suffix for relocation),
// not as a position — so to paint it we locate the quote inside the live
// ProseMirror document and add an inline decoration. The highlight is ephemeral
// UI: it never touches the stored markdown (unlike the `==highlight==` mark).
//
// Decorations are recomputed against the current doc on every editor state
// change, so they survive edits without us mapping positions by hand. The
// plugin state only carries which anchors exist and which one is "active"
// (clicked in the comments panel, or the live draft selection) — pushed in via
// a transaction meta from the React layer (see use-comment-anchors.ts).

import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Node as PMNode } from "@tiptap/pm/model";

export interface CommentAnchor {
  /** Comment id, or the sentinel DRAFT_ANCHOR_ID for the live selection. */
  id: string;
  /** The exact text the comment is attached to. */
  quote: string;
}

export interface AnchorState {
  anchors: CommentAnchor[];
  activeId: string | null;
}

// The in-progress selection being commented on, before any comment exists.
export const DRAFT_ANCHOR_ID = "__draft__";

export const commentAnchorKey = new PluginKey<AnchorState>("noteCommentAnchors");

// Orange, escalating with "active". box-decoration-break:clone keeps the
// rounding intact when a highlight wraps across lines.
const BASE_STYLE =
  "background-color: rgba(251,146,60,0.30); border-radius: 2px; box-decoration-break: clone; -webkit-box-decoration-break: clone;";
const ACTIVE_STYLE =
  "background-color: rgba(251,146,60,0.65); border-radius: 2px; box-decoration-break: clone; -webkit-box-decoration-break: clone;";

// collectText flattens the doc to its visible text, keeping a map from each
// character index back to its ProseMirror position. A block boundary inserts a
// "\n" (mirroring how a DOM selection serialises across paragraphs) mapped to
// the next block's first position, so a quote that spans paragraphs still
// anchors.
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

// findRange locates `quote` in the flattened text and maps it back to doc
// positions. Tries an exact match first, then a whitespace-tolerant match so a
// quote whose spacing was collapsed (soft wraps, normalised runs) still lands.
export function findRange(
  collected: { text: string; map: number[] },
  quote: string,
): { from: number; to: number } | null {
  const q = quote.trim();
  if (!q) return null;
  const { text, map } = collected;
  let idx = text.indexOf(q);
  let len = q.length;
  if (idx < 0) {
    const pattern = escapeRegExp(q).replace(/\s+/g, "\\s+");
    const m = new RegExp(pattern).exec(text);
    if (!m) return null;
    idx = m.index;
    len = m[0].length;
  }
  const last = idx + len - 1;
  const from = map[idx];
  const to = map[last];
  if (from === undefined || to === undefined) return null;
  return { from, to: to + 1 };
}

function buildDecorations(doc: PMNode, state: AnchorState): DecorationSet {
  if (state.anchors.length === 0) return DecorationSet.empty;
  const collected = collectText(doc);
  const decos: Decoration[] = [];
  for (const a of state.anchors) {
    const r = findRange(collected, a.quote);
    if (!r) continue;
    decos.push(
      Decoration.inline(r.from, r.to, {
        style: a.id === state.activeId ? ACTIVE_STYLE : BASE_STYLE,
        class: "note-comment-anchor",
        "data-anchor-id": a.id,
      }),
    );
  }
  return DecorationSet.create(doc, decos);
}

export function commentAnchorPlugin(): Plugin<AnchorState> {
  return new Plugin<AnchorState>({
    key: commentAnchorKey,
    state: {
      init: () => ({ anchors: [], activeId: null }),
      apply(tr, value) {
        const meta = tr.getMeta(commentAnchorKey) as AnchorState | undefined;
        return meta ?? value;
      },
    },
    props: {
      decorations(editorState) {
        const s = commentAnchorKey.getState(editorState);
        if (!s) return null;
        return buildDecorations(editorState.doc, s);
      },
    },
  });
}
