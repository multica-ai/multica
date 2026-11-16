/**
 * Text-anchor caret mapping between the readonly stand-in and the editor.
 *
 * The lazy-editor swap used to land the caret with raw pixel coordinates
 * (`posAtCoords`), which assumes the readonly render and the Tiptap render
 * are pixel-identical. They never are on long documents: per-block height
 * differences (tables, code headers, mention cards) accumulate top-to-bottom,
 * and NodeViews/images keep reflowing after `onReady` — so clicks landed
 * lines away from the target. A text anchor sidesteps layout entirely:
 * record WHICH character of WHICH top-level block was clicked, then resolve
 * that logical position in the ProseMirror document.
 *
 * Offsets count NON-WHITESPACE characters only. The two renderers disagree
 * about whitespace inside container blocks — react-markdown keeps the HTML
 * source's inter-element newlines as text nodes ("URL: x\nTitle: y"), while
 * ProseMirror's textContent concatenates with no separator ("URL: xTitle: y")
 * — so raw character offsets drift one per list item / nested block. The
 * non-whitespace character sequence, however, is identical on both sides.
 * The whitespace ambiguity this creates at word boundaries ("end of this
 * word" vs "start of the next") is resolved by `bias`, captured from what
 * the user actually clicked next to.
 *
 * Known drift: content the two renderers textify differently (a mention's
 * display name is text in the readonly render but an atom — zero text — in
 * the doc) shifts offsets within that one block; resolution clamps to the
 * block end, so worst case the caret lands earlier in the same block, never
 * in another block.
 */

import type { Node as PMNode } from "@tiptap/pm/model";

export interface TextAnchor {
  /** Index of the top-level block (readonly root child ↔ doc child). */
  block: number;
  /** Count of non-whitespace characters before the caret within the block. */
  offset: number;
  /**
   * Which neighbor the caret attaches to when whitespace separates the
   * offset-th non-whitespace character from the next one: 1 = just before
   * the next non-whitespace character (user clicked a line/word start),
   * -1 = just after the previous one (user clicked a line/word end).
   */
  bias: 1 | -1;
}

const NON_WS = /\S/;

function countNonWs(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) if (NON_WS.test(s.charAt(i))) n++;
  return n;
}

/**
 * Resolve a click point inside the readonly render to a text anchor.
 * `root` is the markdown container whose element children correspond 1:1 to
 * the document's top-level blocks. Returns null when the point yields no
 * caret position (unsupported API, click on empty margin below content) —
 * callers fall back to coordinate focus.
 */
export function anchorFromPoint(
  x: number,
  y: number,
  root: HTMLElement,
): TextAnchor | null {
  const doc = root.ownerDocument;
  let node: Node | null = null;
  let nodeOffset = 0;
  // Standard API (Chromium/Firefox), then the WebKit legacy one.
  if (typeof doc.caretPositionFromPoint === "function") {
    const p = doc.caretPositionFromPoint(x, y);
    if (p) {
      node = p.offsetNode;
      nodeOffset = p.offset;
    }
  } else if (typeof doc.caretRangeFromPoint === "function") {
    const r = doc.caretRangeFromPoint(x, y);
    if (r) {
      node = r.startContainer;
      nodeOffset = r.startOffset;
    }
  }
  if (!node || !root.contains(node) || node === root) return null;

  // Climb to the top-level block (the direct child of `root`).
  let blockEl: Node = node;
  while (blockEl.parentNode && blockEl.parentNode !== root) {
    blockEl = blockEl.parentNode;
  }
  if (blockEl.parentNode !== root || blockEl.nodeType !== Node.ELEMENT_NODE) {
    return null;
  }
  const block = Array.prototype.indexOf.call(root.children, blockEl);
  if (block < 0) return null;

  // Text before the caret within the block. Range.toString() handles both
  // text-node carets and element carets (offset = child index), and yields
  // a prefix of the block's textContent — which lets us peek at the char
  // right after the caret for the bias.
  const range = doc.createRange();
  try {
    range.setStart(blockEl, 0);
    range.setEnd(node, nodeOffset);
  } catch {
    return { block, offset: 0, bias: 1 };
  }
  const before = range.toString();
  const nextChar = (blockEl.textContent ?? "").charAt(before.length);
  return {
    block,
    offset: countNonWs(before),
    bias: NON_WS.test(nextChar) ? 1 : -1,
  };
}

/**
 * Resolve a text anchor to a ProseMirror position. Out-of-range values clamp
 * (block → last block, offset → block end) so a stale or drifted anchor still
 * lands inside the intended block instead of throwing.
 */
export function posFromAnchor(doc: PMNode, anchor: TextAnchor): number {
  if (doc.childCount === 0) return 0;
  if (anchor.block >= doc.childCount) return doc.content.size;
  const blockIndex = Math.max(0, anchor.block);

  let blockStart = 0;
  for (let i = 0; i < blockIndex; i++) blockStart += doc.child(i).nodeSize;
  const block = doc.child(blockIndex);
  const contentStart = blockStart + 1;

  const wanted = Math.max(0, anchor.offset);
  let seen = 0;
  // Caret position right after the `wanted`-th non-whitespace character
  // (block content start while none have been passed yet).
  let afterPrev = contentStart;
  let resolved: number | null = null;
  block.descendants((child, pos) => {
    if (resolved !== null) return false;
    if (!child.isText) return true;
    const text = child.text ?? "";
    for (let i = 0; i < text.length; i++) {
      if (!NON_WS.test(text.charAt(i))) continue;
      if (seen === wanted) {
        // This is the first non-whitespace character AFTER the caret.
        resolved = anchor.bias === 1 ? contentStart + pos + i : afterPrev;
        return false;
      }
      seen++;
      afterPrev = contentStart + pos + i + 1;
    }
    return true;
  });
  if (resolved !== null) return resolved;
  // Caret past the last non-whitespace character: exact end lands after it;
  // an overshoot (renderer textified something the doc doesn't, e.g. a
  // mention label) clamps to the block's content end.
  return seen === wanted ? afterPrev : blockStart + block.nodeSize - 1;
}
