export type DiffLineType = "add" | "del" | "context";

export interface DiffLine {
  type: DiffLineType;
  text: string;
}

/**
 * A minimal line-level unified diff via longest-common-subsequence. Kept
 * dependency-free and O(m·n) — tool edits are small — so the UI package doesn't
 * take on a diff library just to render a handful of changed lines.
 */
export function computeLineDiff(oldStr: string, newStr: string): DiffLine[] {
  const a = oldStr === "" ? [] : oldStr.split("\n");
  const b = newStr === "" ? [] : newStr.split("\n");
  const m = a.length;
  const n = b.length;

  // dp[i][j] = LCS length of a[i:] and b[j:].
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i]![j] = a[i] === b[j] ? dp[i + 1]![j + 1]! + 1 : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      out.push({ type: "context", text: a[i]! });
      i++;
      j++;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      out.push({ type: "del", text: a[i]! });
      i++;
    } else {
      out.push({ type: "add", text: b[j]! });
      j++;
    }
  }
  while (i < m) out.push({ type: "del", text: a[i++]! });
  while (j < n) out.push({ type: "add", text: b[j++]! });
  return out;
}

/** Count added / removed lines for a `+X/−Y` summary. */
export function diffStat(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const l of lines) {
    if (l.type === "add") added++;
    else if (l.type === "del") removed++;
  }
  return { added, removed };
}
