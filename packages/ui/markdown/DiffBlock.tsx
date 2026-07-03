import * as React from "react";
import { cn } from "@multica/ui/lib/utils";
import { computeLineDiff, diffStat, type DiffLine } from "./diff";

// Re-export the diff helpers so consumers can import them alongside DiffBlock
// from this module (the sibling ./diff.ts is not reachable through the
// package's "./markdown/*.tsx" export pattern).
export { computeLineDiff, diffStat, type DiffLine, type DiffLineType } from "./diff";

export interface DiffBlockProps {
  /** Prior file contents. Empty string for a newly-written file (all additions). */
  oldString: string;
  /** New file contents. */
  newString: string;
  /** File path shown in the sticky header. */
  filePath?: string;
  className?: string;
}

function gutterGlyph(type: DiffLine["type"]): string {
  if (type === "add") return "+";
  if (type === "del") return "−";
  return " ";
}

/**
 * A lightweight unified-diff view. Added/removed lines carry BOTH a +/−  gutter
 * glyph AND a tinted background (success/destructive at 10% — never a saturated
 * full red/green, never color alone), so the diff stays legible for
 * colorblind users and in both light and dark themes. The file-path header is
 * sticky so it survives scrolling a long diff.
 */
export function DiffBlock({ oldString, newString, filePath, className }: DiffBlockProps): React.JSX.Element {
  const lines = React.useMemo(() => computeLineDiff(oldString, newString), [oldString, newString]);
  const { added, removed } = React.useMemo(() => diffStat(lines), [lines]);

  return (
    <div className={cn("overflow-hidden rounded border text-xs", className)}>
      {filePath && (
        <div className="sticky top-0 z-10 flex items-center justify-between gap-2 border-b bg-muted/60 px-2 py-1 font-mono">
          <span className="truncate" title={filePath}>
            {filePath}
          </span>
          <span className="shrink-0 tabular-nums">
            <span className="text-success">+{added}</span>{" "}
            <span className="text-destructive">−{removed}</span>
          </span>
        </div>
      )}
      <div className="max-h-[240px] overflow-auto font-mono">
        {lines.map((line, idx) => (
          <div
            key={idx}
            className={cn(
              "flex gap-2 whitespace-pre-wrap break-all px-2",
              line.type === "add" && "bg-success/10",
              line.type === "del" && "bg-destructive/10",
            )}
          >
            <span aria-hidden className="w-3 shrink-0 select-none text-center text-muted-foreground">
              {gutterGlyph(line.type)}
            </span>
            <span className="min-w-0 flex-1">{line.text === "" ? " " : line.text}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
