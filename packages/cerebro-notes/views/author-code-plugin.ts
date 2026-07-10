// FIR-2810: author-code stamping. When a note has "Author codes" switched on,
// every line a person writes gets their member code (e.g. "JEH") stamped in
// bold at the start of the line — matching how business reviews are written by
// hand today.
//
// Timing (Jesper, 2026-07-10): the stamp must NOT appear while the line is
// still being written — it fires when the writer is DONE with the line: the
// cursor leaves the block (Enter, click elsewhere, arrow away) or the editor
// blurs. Stamping on the first keystroke broke markdown input rules (typing
// "-" got stamped into "**JEH** -" before the "- " list shortcut could fire)
// and inserted the code mid-thought.
//
// Placement: on a list line the code goes AFTER the marker — "- **JEH** tekst",
// never "**JEH** - tekst". Real bullet blocks (listItem > paragraph) get this
// for free because the marker lives outside the paragraph's content; a literal
// "- " typed as plain text is detected and skipped over explicitly.

import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type { Node as PMNode } from "@tiptap/pm/model";

export const authorCodeKey = new PluginKey<void>("cerebro-note-author-code");

// The shape of a stamp: a short all-caps token ("JEH", "MOP"). Only used
// together with a bold check — plain text starting with an acronym ("AI
// agent…", "KPI i month view") must still be stamped.
const CODE_TOKEN = /^[A-ZÆØÅ]{2,4}$/;

// A literal list marker typed as plain paragraph text ("- ", "* ", "• ").
const LIST_MARKER = /^\s*[-*•]\s+/;

export interface AuthorCodeConfig {
  enabled: boolean;
  code: string;
}

// lineStampOffset decides whether a finished textblock needs a stamp and
// where it goes. Returns the insertion offset relative to the block's content
// start, or null when the block must be left alone. Only paragraphs are
// stamped (headings and code blocks stay clean, matching the reference
// screenshot). A block is skipped when the text after any literal list marker
// already starts with the writer's own code, or with a bold short all-caps
// token — i.e. an existing stamp (someone else's, or pasted).
export function lineStampOffset(block: PMNode, code: string): number | null {
  if (!block.isTextblock || block.type.name !== "paragraph") return null;
  if (block.textContent.trim().length === 0) return null;

  // A literal "- " marker only exists as plain text in the block's first
  // text node; the stamp goes after it. The text that follows the marker is
  // what carries a possible existing stamp.
  let offset = 0;
  let rest: { text: string; bold: boolean } | null = null;
  const first = block.firstChild;
  if (first?.isText && first.text) {
    const marker = LIST_MARKER.exec(first.text);
    const markerLen = marker ? marker[0].length : 0;
    offset = markerLen;
    if (markerLen < first.text.length) {
      rest = {
        text: first.text.slice(markerLen),
        bold: nodeIsBold(first),
      };
    } else {
      // The marker fills the whole first text node ("- " plain, stamp bold
      // in the next node) — look at the node that follows.
      const second = block.childCount > 1 ? block.child(1) : null;
      if (second?.isText && second.text) {
        rest = { text: second.text, bold: nodeIsBold(second) };
      }
    }
  }
  // A marker with nothing written after it yet is not a finished line.
  if (offset > 0 && block.textContent.slice(offset).trim().length === 0) {
    return null;
  }
  if (rest) {
    const token = rest.text.trimStart().split(/\s+/)[0] ?? "";
    if (token === code || rest.text.trimStart() === code) return null;
    if (CODE_TOKEN.test(token) && rest.bold) return null;
  }
  return offset;
}

function nodeIsBold(node: PMNode): boolean {
  return node.marks.some(
    (m) => m.type.name === "bold" || m.type.name === "strong",
  );
}

// buildStampTransaction stamps the block whose content starts at blockStart,
// or returns null when the position no longer points at a stampable block
// (deleted, merged away, already stamped, still empty).
function buildStampTransaction(
  state: EditorState,
  blockStart: number,
  code: string,
): Transaction | null {
  if (blockStart < 0 || blockStart > state.doc.content.size) return null;
  let block: PMNode;
  try {
    const $pos = state.doc.resolve(blockStart);
    block = $pos.parent as PMNode;
    if (!block.isTextblock || $pos.start($pos.depth) !== blockStart) {
      return null;
    }
  } catch {
    return null;
  }
  const offset = lineStampOffset(block, code);
  if (offset === null) return null;

  const at = blockStart + offset;
  const stamp = code + " ";
  const boldMark = state.schema.marks.bold ?? state.schema.marks.strong;
  const tr = state.tr.insertText(stamp, at);
  if (boldMark) {
    tr.addMark(at, at + code.length, boldMark.create());
    // Keep the trailing space (and what follows) unbolded: without this the
    // stored marks from the stamp can bleed into the user's text.
    tr.removeMark(at + code.length, at + stamp.length, boldMark);
    tr.removeStoredMark(boldMark);
  }
  return tr;
}

// authorCodePlugin reads its config through a getter so React can flip the
// toggle / change the code without re-registering the plugin. It keeps one
// piece of per-editor state: the content-start position of the block the user
// is currently writing (a block they took from empty to non-empty). The stamp
// is applied when the cursor leaves that block or the editor blurs.
export function authorCodePlugin(getConfig: () => AuthorCodeConfig): Plugin {
  let pending: number | null = null;

  return new Plugin({
    key: authorCodeKey,
    props: {
      handleDOMEvents: {
        blur: (view) => {
          const { enabled, code } = getConfig();
          if (!enabled || !code || pending === null) return false;
          const tr = buildStampTransaction(view.state, pending, code);
          pending = null;
          if (tr) view.dispatch(tr);
          return false;
        },
      },
    },
    appendTransaction(
      transactions: readonly Transaction[],
      oldState: EditorState,
      newState: EditorState,
    ): Transaction | null {
      const { enabled, code } = getConfig();
      if (!enabled || !code) {
        pending = null;
        return null;
      }
      // Keep the pending block anchored while the doc changes around it.
      if (pending !== null) {
        for (const tr of transactions) {
          if (tr.docChanged) pending = tr.mapping.map(pending);
        }
      }

      const $from = newState.selection.$from;
      const curStart = $from.parent.isTextblock
        ? $from.start($from.depth)
        : null;

      // The cursor left the block being written — the line is finished.
      let out: Transaction | null = null;
      if (pending !== null && pending !== curStart) {
        out = buildStampTransaction(newState, pending, code);
        pending = null;
      }

      // Arm on typing that takes the cursor block from empty to non-empty.
      // Only ever track what the user is typing at the cursor: programmatic
      // rewrites (find & replace, restore, remote refresh) replace whole
      // documents and must not be stamped.
      const docChanged = transactions.some((tr) => tr.docChanged);
      if (
        docChanged &&
        curStart !== null &&
        pending !== curStart &&
        ($from.parent as PMNode).textContent.length > 0
      ) {
        // The block must have been empty (or not existed) before this change —
        // i.e. the user just wrote its first character.
        let oldPos = curStart;
        for (let i = transactions.length - 1; i >= 0; i--) {
          const tr = transactions[i];
          if (tr) oldPos = tr.mapping.invert().map(oldPos);
        }
        let wasEmpty = true;
        if (oldPos >= 0 && oldPos <= oldState.doc.content.size) {
          try {
            const $old = oldState.doc.resolve(oldPos);
            const oldBlock = $old.parent as PMNode;
            if (oldBlock.isTextblock && oldBlock.textContent.length > 0) {
              wasEmpty = false;
            }
          } catch {
            // Unresolvable old position (structure changed) — treat as new.
          }
        }
        if (wasEmpty) pending = curStart;
      }

      return out;
    },
  });
}
