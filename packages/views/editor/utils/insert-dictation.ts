import type { Editor } from "@tiptap/core";
import { Selection } from "@tiptap/pm/state";
import { dictationSeparator } from "@multica/core/voice";

/**
 * Insert dictated text as plain text where the user would type next: over
 * the selection when `atCaret`, otherwise at the end of the document. Line
 * breaks collapse to spaces, and a space goes in front when two words of a
 * spaced language would otherwise touch. Leaves the caret after the text.
 */
export function insertDictation(editor: Editor, text: string, atCaret: boolean): void {
  const clean = text.replace(/\s*\n+\s*/g, " ").trim();
  if (!clean) return;
  // `.to`: when the document ends in a node (an image), atEnd selects that
  // node, and the text goes after it rather than over it.
  const end = Selection.atEnd(editor.state.doc).to;
  const { from, to } = atCaret ? editor.state.selection : { from: end, to: end };
  const before = editor.state.doc.textBetween(Math.max(0, from - 1), from, "\n", " ");
  const insert = dictationSeparator(before, clean) + clean;
  editor
    .chain()
    .insertContentAt({ from, to }, { type: "text", text: insert })
    .focus()
    .run();
}
