"use client";

import { cn } from "@multica/ui/lib/utils";

/** One line of a diff: unchanged, added on the right, or removed from the left. */
export type DiffLine = { kind: "same" | "added" | "removed"; text: string };

/**
 * A line diff of two texts, by longest common subsequence. The inputs are
 * whole texts (instructions, a charter's sections); the output keeps the order
 * of both so a reader sees each change in place. Pure, so it is tested without
 * a DOM.
 */
export function diffLines(before: string, after: string): DiffLine[] {
  const a: string[] = before === "" ? [] : before.split("\n");
  const b: string[] = after === "" ? [] : after.split("\n");
  const n = a.length;
  const m = b.length;
  // lcs[i][j] is the LCS length of a[i:] and b[j:]; row n and column m are zero.
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      const row = lcs[i] ?? [];
      const below = lcs[i + 1] ?? [];
      row[j] = a[i] === b[j] ? (below[j + 1] ?? 0) + 1 : Math.max(below[j] ?? 0, row[j + 1] ?? 0);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    const ai = a[i] ?? "";
    const bj = b[j] ?? "";
    if (ai === bj) {
      out.push({ kind: "same", text: ai });
      i++;
      j++;
    } else if ((lcs[i + 1]?.[j] ?? 0) >= (lcs[i]?.[j + 1] ?? 0)) {
      out.push({ kind: "removed", text: ai });
      i++;
    } else {
      out.push({ kind: "added", text: bj });
      j++;
    }
  }
  while (i < n) out.push({ kind: "removed", text: a[i++] ?? "" });
  while (j < m) out.push({ kind: "added", text: b[j++] ?? "" });
  return out;
}

/**
 * Renders a before-and-after change as a diff. Used for an instruction or
 * charter change in the decisions view, and anywhere else a text changed.
 * Labels come from the caller, so the component carries no copy of its own.
 */
export function DiffView({
  before,
  after,
  removedLabel,
  addedLabel,
  className,
}: {
  before: string;
  after: string;
  removedLabel: string;
  addedLabel: string;
  className?: string;
}) {
  const lines = diffLines(before, after);
  const changed = lines.some((line) => line.kind !== "same");
  return (
    <figure className={cn("flex flex-col gap-2 text-sm", className)}>
      <figcaption className="sr-only">{changed ? `${removedLabel} / ${addedLabel}` : ""}</figcaption>
      <pre className="overflow-x-auto whitespace-pre-wrap rounded-md border bg-muted/30 p-3 font-mono text-xs">
        {lines.map((line, index) => (
          <div
            key={index}
            data-diff-kind={line.kind}
            aria-label={line.kind === "added" ? addedLabel : line.kind === "removed" ? removedLabel : undefined}
            className={cn(
              line.kind === "added" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
              line.kind === "removed" && "bg-red-500/10 text-red-700 line-through dark:text-red-300",
            )}
          >
            {line.kind === "added" ? "+ " : line.kind === "removed" ? "- " : "  "}
            {line.text}
          </div>
        ))}
      </pre>
    </figure>
  );
}
