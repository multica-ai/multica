/**
 * Decides, without a model, whether palette input reads as work to hand to an
 * agent rather than a search. A false positive costs one extra group above the
 * results (and ⇧↵ dismisses it); a false negative leaves the user searching.
 * The rules lean conservative accordingly.
 */

export interface NamedActor {
  type: "agent" | "squad";
  id: string;
  name: string;
}

export type InstructionReason = "names_actor" | "starts_with_verb" | "no_results";

export interface DetectedInstruction {
  reason: InstructionReason;
  /** The agent or squad the input names, if any. */
  actor: NamedActor | null;
}

// Imperative openers. Single CJK characters that begin ordinary nouns
// ("加密", "写作") are left out; "让 / 请 / 帮" almost only open a request.
const LATIN_VERBS = new Set([
  "fix",
  "add",
  "create",
  "make",
  "write",
  "build",
  "implement",
  "refactor",
  "investigate",
  "debug",
  "review",
  "help",
  "please",
  "design",
  "draft",
  "improve",
  "optimize",
  "migrate",
  "research",
  "analyze",
  "analyse",
  "summarize",
  "document",
  "let",
  "ask",
  "tell",
  // fr
  "corrige",
  "ajoute",
  "crée",
  "cree",
  "écris",
  "ecris",
  "implémente",
  "analyse",
  "optimise",
  "aide",
]);

const CJK_VERBS = [
  "让",
  "請",
  "请",
  "帮",
  "幫",
  "麻烦",
  "麻煩",
  "修复",
  "修復",
  "修一下",
  "改一下",
  "实现",
  "實現",
  "添加",
  "新增",
  "加一个",
  "加一個",
  "写一个",
  "寫一個",
  "优化",
  "優化",
  "调查",
  "調查",
  "排查",
  "重构",
  "重構",
  "设计",
  "設計",
  "检查",
  "檢查",
  "处理",
  "處理",
  "整理",
  "分析",
  "总结",
  "總結",
  "研究",
  "迁移",
  "遷移",
];

const CJK = /[\u3040-\u30ff\u3400-\u9fff\uac00-\ud7af]/;
const PUNCTUATION = /[\s,.!?;:，。！？；：、"'“”‘’()（）]+/g;

function isLatinWordChar(ch: string | undefined) {
  return !!ch && /[\p{L}\p{N}_-]/u.test(ch) && !CJK.test(ch);
}

/** Where `name` occurs in `input` as a whole word (for Latin names), or -1. */
function findName(input: string, name: string): number {
  const haystack = input.toLowerCase();
  const needle = name.toLowerCase();
  let from = 0;
  while (from <= haystack.length) {
    const at = haystack.indexOf(needle, from);
    if (at === -1) return -1;
    const before = haystack[at - 1];
    const after = haystack[at + needle.length];
    if (!isLatinWordChar(before) && !isLatinWordChar(after)) return at;
    from = at + 1;
  }
  return -1;
}

function meaningfulLength(text: string) {
  return text.replace(PUNCTUATION, "").length;
}

function startsWithVerb(input: string): boolean {
  const lower = input.toLowerCase();
  const tokens = lower.split(/\s+/).filter(Boolean);
  const first = tokens[0]?.replace(/[,.!?:;]+$/, "");
  if (first && LATIN_VERBS.has(first)) return tokens.length >= 2;
  for (const verb of CJK_VERBS) {
    if (lower.startsWith(verb)) return meaningfulLength(lower.slice(verb.length)) >= 2;
  }
  return false;
}

function isInstructionRemainder(rest: string): boolean {
  if (CJK.test(rest)) return meaningfulLength(rest) >= 2;
  return rest.split(PUNCTUATION).filter(Boolean).length >= 2;
}

function isMultiWord(input: string): boolean {
  const tokens = input.split(/\s+/).filter(Boolean);
  if (tokens.length >= 2) return true;
  // CJK text has no spaces; four characters is already a phrase.
  return CJK.test(input) && meaningfulLength(input) >= 4;
}

export function detectInstruction(
  rawInput: string,
  actors: readonly NamedActor[],
  options: { hasResults: boolean; resultsSettled: boolean },
): DetectedInstruction | null {
  const input = rawInput.trim();
  if (meaningfulLength(input) < 4) return null;

  // Longest name first, so "Lambda Pro" wins over "Lambda".
  const byLength = [...actors]
    .filter((actor) => actor.name.trim().length >= 2)
    .sort((a, b) => b.name.length - a.name.length);
  for (const actor of byLength) {
    const at = findName(input, actor.name);
    if (at === -1) continue;
    const rest = input.slice(0, at) + " " + input.slice(at + actor.name.length);
    // The name alone, or with one more word ("lambda runs"), is a search for
    // the agent, not an instruction to it.
    if (isInstructionRemainder(rest)) return { reason: "names_actor", actor };
  }

  if (startsWithVerb(input)) return { reason: "starts_with_verb", actor: null };

  if (options.resultsSettled && !options.hasResults && isMultiWord(input)) {
    return { reason: "no_results", actor: null };
  }
  return null;
}
