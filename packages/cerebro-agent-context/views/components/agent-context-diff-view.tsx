"use client";

// Unified-diff-style view for an agent's instructions text. No external diff
// library — line-level LCS, mirroring cerebro-skill-ownership's SkillDiffView.

type DiffLine =
  | { type: "unchanged"; text: string }
  | { type: "added"; text: string }
  | { type: "removed"; text: string };

function computeDiff(base: string, proposed: string): DiffLine[] {
  const baseLines = base.split("\n");
  const proposedLines = proposed.split("\n");
  const m = baseLines.length;
  const n = proposedLines.length;

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

  const result: DiffLine[] = [];
  let i = m;
  let j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && baseLines[i - 1] === proposedLines[j - 1]) {
      result.push({ type: "unchanged", text: baseLines[i - 1]! });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i]![j - 1]! >= dp[i - 1]![j]!)) {
      result.push({ type: "added", text: proposedLines[j - 1]! });
      j--;
    } else {
      result.push({ type: "removed", text: baseLines[i - 1]! });
      i--;
    }
  }
  result.reverse();
  return result;
}

interface Props {
  base: string;
  proposed: string;
  baseLabel?: string;
  proposedLabel?: string;
}

export function AgentContextDiffView({
  base,
  proposed,
  baseLabel = "base",
  proposedLabel = "proposed",
}: Props) {
  const diff = computeDiff(base, proposed);
  const hasChanges = diff.some((l) => l.type !== "unchanged");

  if (!hasChanges) {
    return (
      <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        No differences between {baseLabel} and {proposedLabel}.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-md border bg-muted/20 font-mono text-xs leading-5">
      <div className="flex items-center gap-4 border-b px-3 py-1.5 text-xs text-muted-foreground">
        <span className="text-destructive/70">− {baseLabel}</span>
        <span className="text-emerald-600">+ {proposedLabel}</span>
      </div>
      <div className="max-h-72 overflow-y-auto">
        {diff.map((line, i) => (
          <div
            key={i}
            className={
              line.type === "added"
                ? "bg-emerald-500/10 pl-6 pr-2 text-emerald-700 dark:text-emerald-400"
                : line.type === "removed"
                  ? "bg-destructive/10 pl-6 pr-2 text-destructive/80"
                  : "px-6 pr-2 text-muted-foreground"
            }
          >
            <span className="mr-2 select-none">
              {line.type === "added" ? "+" : line.type === "removed" ? "−" : " "}
            </span>
            {line.text || " "}
          </div>
        ))}
      </div>
    </div>
  );
}
