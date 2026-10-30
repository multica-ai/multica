import type { SkillFiles } from "./fixtures";

export type DiffLine = {
  kind: "context" | "add" | "del";
  text: string;
  oldNo: number | null;
  newNo: number | null;
};

export type FileStatus = "added" | "removed" | "modified" | "unchanged";

export type FileDiff = {
  path: string;
  status: FileStatus;
  lines: DiffLine[];
  additions: number;
  deletions: number;
};

/** Line-level LCS diff. Skill files are small, so O(n·m) is fine here. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = before === "" ? [] : before.split("\n");
  const b = after === "" ? [] : after.split("\n");
  const lcs = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--)
    for (let j = b.length - 1; j >= 0; j--)
      lcs[i]![j] =
        a[i] === b[j]
          ? lcs[i + 1]![j + 1]! + 1
          : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      out.push({ kind: "context", text: a[i]!, oldNo: i + 1, newNo: j + 1 });
      i++;
      j++;
    } else if (
      j >= b.length ||
      (i < a.length && lcs[i + 1]![j]! >= lcs[i]![j + 1]!)
    ) {
      out.push({ kind: "del", text: a[i]!, oldNo: i + 1, newNo: null });
      i++;
    } else {
      out.push({ kind: "add", text: b[j]!, oldNo: null, newNo: j + 1 });
      j++;
    }
  }
  return out;
}

/** SKILL.md first, then the supporting files by path — the file-tree order. */
export function comparePaths(x: string, y: string): number {
  if (x === "SKILL.md") return -1;
  if (y === "SKILL.md") return 1;
  return x.localeCompare(y);
}

export function diffSkill(from: SkillFiles, to: SkillFiles): FileDiff[] {
  const paths = [...new Set([...Object.keys(from), ...Object.keys(to)])].sort(
    comparePaths,
  );
  return paths.map((path) => {
    const before = from[path];
    const after = to[path];
    const status: FileStatus =
      before === undefined
        ? "added"
        : after === undefined
          ? "removed"
          : before === after
            ? "unchanged"
            : "modified";
    const lines = diffLines(before ?? "", after ?? "");
    return {
      path,
      status,
      lines,
      additions: lines.filter((line) => line.kind === "add").length,
      deletions: lines.filter((line) => line.kind === "del").length,
    };
  });
}
