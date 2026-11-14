import type { Editor } from "@tiptap/core";
import { Selection } from "@tiptap/pm/state";
import { dictationSeparator } from "@multica/core/voice";

/**
 * Insert dictated text as plain text where the user would type next: over
 * the selection when `atCaret`, otherwise at the end of the document. Line
 * breaks collapse to spaces, and a space goes on either side where two words
 * of a spaced language would otherwise touch. Leaves the caret right after
 * the dictated words.
 */
export function insertDictation(editor: Editor, text: string, atCaret: boolean): void {
  const clean = text.replace(/\s*\n+\s*/g, " ").trim();
  if (!clean) return;
  // `.to`: when the document ends in a node (an image), atEnd selects that
  // node, and the text goes after it rather than over it.
  const { doc } = editor.state;
  const end = Selection.atEnd(doc).to;
  const { from, to } = atCaret ? editor.state.selection : { from: end, to: end };
  const before = doc.textBetween(Math.max(0, from - 1), from, "\n", " ");
  const after = doc.textBetween(to, Math.min(doc.content.size, to + 1), "\n", " ");
  const lead = dictationSeparator(before, clean) + clean;
  editor
    .chain()
    .insertContentAt({ from, to }, { type: "text", text: lead + dictationSeparator(clean, after) })
    .setTextSelection(from + lead.length)
    .focus()
    .run();
}
