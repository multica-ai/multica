// FIR-2810: author-code stamping. When a note has "Author codes" switched on,
// every line a person writes gets their member code (e.g. "JEH") stamped in
// bold at the start of the line — matching how business reviews are written by
// hand today. The stamp fires when a paragraph transitions from empty to
// non-empty (the user just typed the first character of a new line), so
// existing lines and template boilerplate are never touched retroactively.

import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type { Node as PMNode } from "@tiptap/pm/model";

export const authorCodeKey = new PluginKey<void>("cerebro-note-author-code");

// The shape of a stamp: a short all-caps token ("JEH", "MOP"). Only used
// together with a bold check — plain text starting with an acronym ("AI
// agent…", "KPI i month view") must still be stamped.
const CODE_TOKEN = /^[A-ZÆØÅ]{2,4}$/;

export interface AuthorCodeConfig {
  enabled: boolean;
  code: string;
}

// shouldStampBlock decides whether a textblock that now contains text needs a
// stamp. Pure so it can be unit-tested without a live editor: only paragraphs
// are stamped (headings and code blocks stay clean, matching the reference
// screenshot). A block is left alone when it already starts with the writer's
// own code, or when its first text is a bold short all-caps token — i.e. an
// existing stamp (someone else's, or pasted). hasBoldCodePrefix carries that
// mark-level check, which the pure text cannot see.
export function shouldStampBlock(
  typeName: string,
  text: string,
  code: string,
  hasBoldCodePrefix: boolean,
): boolean {
  if (typeName !== "paragraph") return false;
  const trimmed = text.trimStart();
  if (trimmed.length === 0) return false;
  if (trimmed.startsWith(code + " ") || trimmed === code) return false;
  if (hasBoldCodePrefix) return false;
  return true;
}

// blockHasBoldCodePrefix inspects the first text node of a block: bold + a
// short all-caps token means the line already carries a member-code stamp.
export function blockHasBoldCodePrefix(block: PMNode): boolean {
  const first = block.firstChild;
  if (!first || !first.isText || !first.text) return false;
  const token = first.text.trim().split(/\s+/)[0] ?? "";
  if (!CODE_TOKEN.test(token)) return false;
  return first.marks.some(
    (m) => m.type.name === "bold" || m.type.name === "strong",
  );
}

// authorCodePlugin reads its config through a getter so React can flip the
// toggle / change the code without re-registering the plugin.
export function authorCodePlugin(getConfig: () => AuthorCodeConfig): Plugin {
  return new Plugin({
    key: authorCodeKey,
    appendTransaction(
      transactions: readonly Transaction[],
      oldState: EditorState,
      newState: EditorState,
    ): Transaction | null {
      const { enabled, code } = getConfig();
      if (!enabled || !code) return null;
      if (!transactions.some((tr) => tr.docChanged)) return null;
      // Only ever stamp what the user is typing at the cursor: the block the
      // selection sits in. Programmatic rewrites (find & replace, restore,
      // remote refresh) replace whole documents and must not be stamped.
      const $from = newState.selection.$from;
      const block = $from.parent as PMNode;
      if (!block.isTextblock) return null;
      if (
        !shouldStampBlock(
          block.type.name,
          block.textContent,
          code,
          blockHasBoldCodePrefix(block),
        )
      ) {
        return null;
      }
      // The block must have been empty (or not existed) before this change —
      // i.e. the user just wrote its first character.
      const blockStart = $from.start($from.depth);
      let oldPos = blockStart;
      for (let i = transactions.length - 1; i >= 0; i--) {
        const tr = transactions[i];
        if (tr) oldPos = tr.mapping.invert().map(oldPos);
      }
      if (oldPos >= 0 && oldPos <= oldState.doc.content.size) {
        try {
          const $old = oldState.doc.resolve(oldPos);
          const oldBlock = $old.parent as PMNode;
          if (oldBlock.isTextblock && oldBlock.textContent.length > 0) {
            return null; // the line already had content before this edit
          }
        } catch {
          // Unresolvable old position (structure changed) — treat as new line.
        }
      }
      const boldMark =
        newState.schema.marks.bold ?? newState.schema.marks.strong;
      const stamp = code + " ";
      const tr = newState.tr.insertText(stamp, blockStart);
      if (boldMark) {
        tr.addMark(blockStart, blockStart + code.length, boldMark.create());
        // Keep the just-typed text (and what follows) unbolded: without this
        // the stored marks from the stamp can bleed into the user's text.
        tr.removeMark(
          blockStart + code.length,
          blockStart + stamp.length,
          boldMark,
        );
        tr.removeStoredMark(boldMark);
      }
      return tr;
    },
  });
}
