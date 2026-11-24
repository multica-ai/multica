export interface TextEdit {
  old_text: string;
  new_text: string;
  replace_all?: boolean;
}

export type EditFailure =
  | { reason: "not_found"; index: number }
  | { reason: "ambiguous"; index: number; matches: number }
  | { reason: "empty"; index: number };

export type EditResult = { ok: true; text: string } | ({ ok: false } & EditFailure);

function countOccurrences(text: string, needle: string): number {
  let count = 0;
  for (let at = text.indexOf(needle); at !== -1; at = text.indexOf(needle, at + needle.length)) {
    count++;
  }
  return count;
}

/**
 * Applies find-and-replace edits in order. Each `old_text` must occur exactly
 * once (or at least once with `replace_all`) in the text as the previous
 * edits left it. This is what lets an agent edit a document others are
 * changing: the quoted text is the base, so the edit lands wherever that text
 * now is and fails only when someone changed that very text.
 */
export function applyTextEdits(text: string, edits: TextEdit[]): EditResult {
  let result = text;
  for (const [index, edit] of edits.entries()) {
    if (!edit.old_text) return { ok: false, reason: "empty", index };
    const matches = countOccurrences(result, edit.old_text);
    if (matches === 0) return { ok: false, reason: "not_found", index };
    if (matches > 1 && !edit.replace_all) {
      return { ok: false, reason: "ambiguous", index, matches };
    }
    result = edit.replace_all
      ? result.split(edit.old_text).join(edit.new_text)
      : result.replace(edit.old_text, () => edit.new_text);
  }
  return { ok: true, text: result };
}
