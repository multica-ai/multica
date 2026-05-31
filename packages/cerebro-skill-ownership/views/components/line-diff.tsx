"use client";

/**
 * Minimal unified line diff. The repo ships no diff viewer, so this renders a
 * line-level longest-common-subsequence diff using the app's existing tokens
 * (added = success tint, removed = destructive tint). Good enough to review a
 * change request's content delta; it is read-only and never edits either side.
 */

type DiffLine = { type: "add" | "del" | "ctx"; text: string };

function diffLines(before: string, after: string): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  const n = a.length;
  const m = b.length;

  // LCS table.
  const lcs: number[][] = Array.from({ length: n + 1 }, () =>
    new Array<number>(m + 1).fill(0),
  );
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i]![j] =
        a[i] === b[j]
          ? lcs[i + 1]![j + 1]! + 1
          : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
    }
  }

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ type: "ctx", text: a[i]! });
      i++;
      j++;
    } else if (lcs[i + 1]![j]! >= lcs[i]![j + 1]!) {
      out.push({ type: "del", text: a[i]! });
      i++;
    } else {
      out.push({ type: "add", text: b[j]! });
      j++;
    }
  }
  while (i < n) out.push({ type: "del", text: a[i++]! });
  while (j < m) out.push({ type: "add", text: b[j++]! });
  return out;
}

export function LineDiff({
  before,
  after,
}: {
  before: string;
  after: string;
}) {
  if (before === after) {
    return (
      <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        Ingen ændringer i indholdet.
      </div>
    );
  }

  const lines = diffLines(before, after);
  return (
    <div className="max-h-80 overflow-auto rounded-md border bg-muted/20 font-mono text-xs">
      {lines.map((line, idx) => (
        <div
          key={idx}
          className={
            line.type === "add"
              ? "flex gap-2 bg-success/10 px-2 py-0.5 text-success"
              : line.type === "del"
                ? "flex gap-2 bg-destructive/10 px-2 py-0.5 text-destructive"
                : "flex gap-2 px-2 py-0.5 text-muted-foreground"
          }
        >
          <span aria-hidden className="select-none">
            {line.type === "add" ? "+" : line.type === "del" ? "-" : " "}
          </span>
          <span className="whitespace-pre-wrap break-all">
            {line.text || " "}
          </span>
        </div>
      ))}
    </div>
  );
}
