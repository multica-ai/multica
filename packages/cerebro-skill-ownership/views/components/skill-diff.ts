// Pure line-level diff + hunk-collapsing logic for SKILL.md content.
// No external diff library — LCS-based, mirrors cerebro-agent-context's
// AgentContextDiffView. Kept framework-free so it can be unit tested without
// rendering React.

export type DiffLine =
  | { type: "unchanged"; text: string; oldLine: number; newLine: number }
  | { type: "added"; text: string; oldLine: null; newLine: number }
  | { type: "removed"; text: string; oldLine: number; newLine: null };

export function computeDiff(base: string, proposed: string): DiffLine[] {
  const baseLines = base.split("\n");
  const proposedLines = proposed.split("\n");

  // LCS-based diff (simplified: patience-like approach with a small table).
  // For skill files this is fast enough.
  const m = baseLines.length;
  const n = proposedLines.length;

  // Build LCS length table
  const dp: number[][] = Array.from({ length: m + 1 }, () =>
    new Array<number>(n + 1).fill(0),
  );
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (baseLines[i - 1] === proposedLines[j - 1]) {
        dp[i]![j] = dp[i - 1]![j - 1]! + 1;
      } else {
        dp[i]![j] = Math.max(dp[i - 1]![j]!, dp[i]![j - 1]!);
      }
    }
  }

  // Trace back
  type RawLine =
    | { type: "unchanged"; text: string }
    | { type: "added"; text: string }
    | { type: "removed"; text: string };
  const raw: RawLine[] = [];
  let i = m;
  let j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && baseLines[i - 1] === proposedLines[j - 1]) {
      raw.push({ type: "unchanged", text: baseLines[i - 1]! });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i]![j - 1]! >= dp[i - 1]![j]!)) {
      raw.push({ type: "added", text: proposedLines[j - 1]! });
      j--;
    } else {
      raw.push({ type: "removed", text: baseLines[i - 1]! });
      i--;
    }
  }
  raw.reverse();

  // Forward pass to attach old/new line numbers.
  let oldLine = 0;
  let newLine = 0;
  return raw.map((line): DiffLine => {
    if (line.type === "unchanged") {
      oldLine++;
      newLine++;
      return { type: "unchanged", text: line.text, oldLine, newLine };
    }
    if (line.type === "added") {
      newLine++;
      return { type: "added", text: line.text, oldLine: null, newLine };
    }
    oldLine++;
    return { type: "removed", text: line.text, oldLine, newLine: null };
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
