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

// Hesitations that carry no meaning on their own. Chinese ones are only
// dropped when they stand alone between punctuation, so 嗯哼 survives.
const SPOKEN_FILLERS = [
  /(^|[\s,.!?;:，。！？；：、])(?:嗯+|呃+|额+)(?=[\s,.!?;:，。！？；：、]|$)[，、,]?\s*/gu,
  /(^|[\s,.!?;:])(?:u+m+|u+h+|e+r+m+|h+m+)(?=[\s,.!?;:]|$)[,]?\s*/giu,
];

/** Remove hesitation words ("um", "uh", "嗯", "呃") from a transcript. */
export function removeSpokenFillers(text: string): string {
  let out = text;
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
