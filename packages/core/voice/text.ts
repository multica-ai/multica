const CJK = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uff00-\uffef]/;

/**
 * The separator to put between text already in the draft and dictated text
 * landing right after it: a space between two words of a spaced language,
 * nothing where either side is CJK or the draft already ends in whitespace.
 */
export function dictationSeparator(before: string, text: string): string {
  if (!before || !text) return "";
  const last = before[before.length - 1]!;
  const first = text[0]!;
  if (/\s/.test(last) || /\s/.test(first)) return "";
  if (CJK.test(last) || CJK.test(first)) return "";
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

/** The custom vocabulary as the hint transcription models accept. */
export function vocabularyPrompt(words: readonly string[]): string {
  return words
    .map((word) => word.trim())
    .filter(Boolean)
    .join(", ");
}
