/**
 * Names the speech model should favour: the user's vocabulary plus the
 * current workspace's people, agents and projects. Models that take hotwords
 * spell these right far more often than post-correction can fix them.
 */

/** Each word lengthens the model's prompt; past this, decoding slows with little gain. */
export const MAX_VOICE_HOTWORDS = 60;

let contextWords: readonly string[] = [];

/** Replace the workspace names offered as hotwords; the app keeps this current. */
export function setVoiceContextWords(words: readonly string[]) {
  contextWords = [...words];
}

/** Vocabulary first, then context names; deduped case-insensitively and capped. */
export function voiceHotwords(vocabulary: readonly string[], context: readonly string[] = contextWords): string[] {
  const seen = new Set<string>();
  const words: string[] = [];
  for (const raw of [...vocabulary, ...context]) {
    const word = raw.trim();
    const key = word.toLowerCase();
    if (!word || seen.has(key)) continue;
    seen.add(key);
    words.push(word);
    if (words.length >= MAX_VOICE_HOTWORDS) break;
  }
  return words;
}
