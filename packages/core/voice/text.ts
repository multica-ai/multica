const CJK = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uff00-\uffef]/;

const OPENING = /[([{“‘«¿¡]/;
const CLOSING = /[)\]}”’».,!?;:%…'"]/;

/**
 * The separator to put between two adjacent pieces of text when dictation
 * joins them (draft then transcript, or transcript then draft): a space
 * between two words of a spaced language, nothing where either side is CJK,
 * whitespace already separates them, or punctuation hugs the word.
 */
export function dictationSeparator(before: string, after: string): string {
  if (!before || !after) return "";
  const last = before[before.length - 1]!;
  const first = after[0]!;
  if (/\s/.test(last) || /\s/.test(first)) return "";
  if (CJK.test(last) || CJK.test(first)) return "";
  if (OPENING.test(last) || CLOSING.test(first)) return "";
  return " ";
}

// Hesitations that carry no meaning on their own. 额 is also part of words
// (金额, 额外), so it only goes when it stands alone between punctuation.
const SPOKEN_FILLERS = [
  /(^|[\s,.!?;:，。！？；：、])额+(?=[\s,.!?;:，。！？；：、]|$)[，、,]?\s*/gu,
  /(^|[\s,.!?;:])(?:u+m+|u+h+|e+r+m+|h+m+)(?=[\s,.!?;:]|$)[,]?\s*/giu,
];

// 呃 and 嗯 are hesitations wherever they land, glued to the words around them
// too ("就是呃，", "那个呃标识"), with the pause the model wrote after them.
// 呃逆 and 嗯哼 are words.
const GLUED_FILLER = /(?:呃(?!逆)|嗯(?!哼))+(?:[，、,]\s*|\s+)?/gu;
const BREAK = /[\s\p{P}]/u;

/** Remove hesitation words ("um", "uh", "嗯", "呃") from a transcript. */
export function removeSpokenFillers(text: string): string {
  let out = text.replace(GLUED_FILLER, (match, offset: number, whole: string) => {
    const before = whole[offset - 1];
    const after = whole[offset + match.length];
    if (!before || !after || BREAK.test(before) || BREAK.test(after)) return "";
    // Words on both sides: keep them apart, except between two CJK characters.
    return CJK.test(before) && CJK.test(after) ? "" : " ";
  });
  for (const filler of SPOKEN_FILLERS) out = out.replace(filler, "$1");
  return out
    .replace(/\s{2,}/g, " ")
    .replace(/^[\s,，、]+/, "")
    .trim();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Spell vocabulary words the way the user wrote them: "multica" becomes
 * "Multica", and a CamelCase word also matches its spoken split, so "git hub"
 * becomes "GitHub". Only whole words are replaced.
 */
export function applyVocabulary(text: string, words: readonly string[]): string {
  let out = text;
  for (const word of words) {
    const trimmed = word.trim();
    if (!/\p{L}/u.test(trimmed) || trimmed.toLowerCase() === trimmed.toUpperCase()) continue;
    const parts = trimmed
      .split(/\s+|(?<=\p{Ll})(?=\p{Lu})/u)
      .filter(Boolean)
      .map(escapeRegExp);
    const pattern = new RegExp(`(?<![\\p{L}\\p{N}])${parts.join("[\\s-]?")}(?![\\p{L}\\p{N}])`, "giu");
    out = out.replace(pattern, trimmed);
  }
  return out;
}
