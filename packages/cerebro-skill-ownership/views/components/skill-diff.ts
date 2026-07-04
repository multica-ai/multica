// Diff + hunk-collapsing logic for SKILL.md content (prose, not code).
// No external diff library — LCS-based. Word-level highlighting on modified
// lines is what makes this readable for prose (mirrors how text-focused diff
// tools like Mergely use word/character diffing instead of line-only
// red/green blocks — see FIR-2684).

export interface WordSegment {
  text: string;
  changed: boolean;
}

export type DiffLine =
  | { type: "unchanged"; text: string; oldLine: number; newLine: number }
  | {
      type: "added";
      text: string;
      oldLine: null;
      newLine: number;
      words?: WordSegment[];
    }
  | {
      type: "removed";
      text: string;
      oldLine: number;
      newLine: null;
      words?: WordSegment[];
    };

type LcsOp<T> = { type: "unchanged" | "added" | "removed"; value: T };

// Generic LCS-based diff over any array of comparable tokens (lines or
// words). Small enough inputs (a skill file's lines, or one line's words)
// that the O(m*n) DP table is fast.
function lcsDiff<T>(a: T[], b: T[]): LcsOp<T>[] {
  const m = a.length;
  const n = b.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () =>
    new Array<number>(n + 1).fill(0),
  );
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (a[i - 1] === b[j - 1]) {
        dp[i]![j] = dp[i - 1]![j - 1]! + 1;
      } else {
        dp[i]![j] = Math.max(dp[i - 1]![j]!, dp[i]![j - 1]!);
      }
    }
  }

  const result: LcsOp<T>[] = [];
  let i = m;
  let j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && a[i - 1] === b[j - 1]) {
      result.push({ type: "unchanged", value: a[i - 1]! });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i]![j - 1]! >= dp[i - 1]![j]!)) {
      result.push({ type: "added", value: b[j - 1]! });
      j--;
    } else {
      result.push({ type: "removed", value: a[i - 1]! });
      i--;
    }
  }
  result.reverse();
  return result;
}

// Splits a line into words + whitespace runs so re-joining the tokens
// reconstructs the original text exactly.
function tokenizeWords(text: string): string[] {
  return text.match(/\S+|\s+/g) ?? [];
}

// Only treat a removed/added line pair as "the same line, edited" (and word-
// diff it) when they still share a meaningful fraction of their words.
// Otherwise they're two unrelated lines that happen to be adjacent in a
// replace hunk, and word-diffing them would highlight almost everything.
// Similarity is measured over non-whitespace tokens only — whitespace runs
// (the spaces between words) match trivially between almost any two lines
// of similar length and would otherwise inflate the score.
const MIN_WORD_SIMILARITY = 0.5;

function wordDiff(oldText: string, newText: string): { old: WordSegment[]; next: WordSegment[] } | null {
  const oldTokens = tokenizeWords(oldText);
  const newTokens = tokenizeWords(newText);
  const ops = lcsDiff(oldTokens, newTokens);
  const unchangedWordCount = ops.filter((op) => op.type === "unchanged" && op.value.trim() !== "").length;
  const oldWordCount = oldTokens.filter((t) => t.trim() !== "").length;
  const newWordCount = newTokens.filter((t) => t.trim() !== "").length;
  const similarity = unchangedWordCount / Math.max(oldWordCount, newWordCount, 1);
  if (similarity < MIN_WORD_SIMILARITY) return null;

  const old: WordSegment[] = [];
  const next: WordSegment[] = [];
  for (const op of ops) {
    if (op.type === "unchanged") {
      old.push({ text: op.value, changed: false });
      next.push({ text: op.value, changed: false });
    } else if (op.type === "removed") {
      old.push({ text: op.value, changed: true });
    } else {
      next.push({ text: op.value, changed: true });
    }
  }
  return { old, next };
}

export function computeDiff(base: string, proposed: string): DiffLine[] {
  const baseLines = base.split("\n");
  const proposedLines = proposed.split("\n");
  const ops = lcsDiff(baseLines, proposedLines);

  // Pair up consecutive "removed" lines with consecutive "added" lines
  // (a replace hunk) 1:1 by index, and word-diff each pair, so an edited
  // sentence highlights only the words that changed instead of turning the
  // whole line into a solid red/green block.
  const wordsForIndex = new Map<number, WordSegment[]>();
  let k = 0;
  while (k < ops.length) {
    if (ops[k]!.type !== "removed") {
      k++;
      continue;
    }
    const removedStart = k;
    while (k < ops.length && ops[k]!.type === "removed") k++;
    const addedStart = k;
    while (k < ops.length && ops[k]!.type === "added") k++;
    const removedCount = addedStart - removedStart;
    const addedCount = k - addedStart;
    const pairCount = Math.min(removedCount, addedCount);
    for (let p = 0; p < pairCount; p++) {
      const removedIdx = removedStart + p;
      const addedIdx = addedStart + p;
      const diff = wordDiff(ops[removedIdx]!.value, ops[addedIdx]!.value);
      if (diff) {
        wordsForIndex.set(removedIdx, diff.old);
        wordsForIndex.set(addedIdx, diff.next);
      }
    }
  }

  // Forward pass to attach old/new line numbers + word segments.
  let oldLine = 0;
  let newLine = 0;
  return ops.map((op, idx): DiffLine => {
    if (op.type === "unchanged") {
      oldLine++;
      newLine++;
      return { type: "unchanged", text: op.value, oldLine, newLine };
    }
    if (op.type === "added") {
      newLine++;
      return { type: "added", text: op.value, oldLine: null, newLine, words: wordsForIndex.get(idx) };
    }
    oldLine++;
    return { type: "removed", text: op.value, oldLine, newLine: null, words: wordsForIndex.get(idx) };
  });
}

// How many unchanged lines to keep as context around a change before
// collapsing the rest into an expandable divider.
export const DIFF_CONTEXT_LINES = 3;

export type RenderGroup =
  | { kind: "line"; line: DiffLine; key: number }
  | { kind: "collapsed"; lines: DiffLine[]; key: number };

export function groupForDisplay(diff: DiffLine[]): RenderGroup[] {
  const nearChange = new Array(diff.length).fill(false);
  diff.forEach((line, idx) => {
    if (line.type === "unchanged") return;
    for (
      let k = Math.max(0, idx - DIFF_CONTEXT_LINES);
      k <= Math.min(diff.length - 1, idx + DIFF_CONTEXT_LINES);
      k++
    ) {
      nearChange[k] = true;
    }
  });

  const groups: RenderGroup[] = [];
  let idx = 0;
  while (idx < diff.length) {
    if (diff[idx]!.type === "unchanged" && !nearChange[idx]) {
      const start = idx;
      while (idx < diff.length && diff[idx]!.type === "unchanged" && !nearChange[idx]) {
        idx++;
      }
      groups.push({ kind: "collapsed", lines: diff.slice(start, idx), key: start });
    } else {
      groups.push({ kind: "line", line: diff[idx]!, key: idx });
      idx++;
    }
  }
  return groups;
}
