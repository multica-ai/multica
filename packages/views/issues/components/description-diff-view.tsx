import { useMemo } from "react";
import { diffDescriptions, type DiffLine } from "@multica/core/issues/description-diff";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

interface DescriptionDiffViewProps {
  /** Snapshot being compared against. */
  before: string;
  /** Snapshot as of this version. */
  after: string;
  /**
   * Caps the rendered rows so a long diff inside a timeline entry stays a
   * preview rather than swallowing the thread. The history panel passes no
   * limit and shows everything.
   */
  maxLines?: number;
  className?: string;
}

/**
 * Unified diff over the raw markdown.
 *
 * Source, not rendered markdown: the audience here reads pull requests, and a
 * source diff is both the familiar shape and the honest one — a rendered diff
 * hides exactly the structural edits (heading level, list nesting, link target)
 * that matter most when reviewing what an agent changed.
 */
export function DescriptionDiffView({
  before,
  after,
  maxLines,
  className,
}: DescriptionDiffViewProps) {
  const { t } = useT("issues");
  const diff = useMemo(() => diffDescriptions(before, after), [before, after]);

  if (diff.isEmpty) {
    return (
      <p className={cn("text-xs text-muted-foreground", className)}>
        {t(($) => $.description_history.empty_both_sides)}
      </p>
    );
  }

  // A wholesale rewrite makes a unified diff useless — every line reads as
  // removed and re-added. Agents replacing a description outright make this
  // common, so it gets a real presentation rather than a degraded diff.
  if (diff.isRewrite) {
    return (
      <div className={cn("grid gap-2 sm:grid-cols-2", className)}>
        <RewriteSide
          label={t(($) => $.description_history.rewrite_before)}
          content={before}
          tone="removed"
          emptyLabel={t(($) => $.description_history.empty_side)}
        />
        <RewriteSide
          label={t(($) => $.description_history.rewrite_after)}
          content={after}
          tone="added"
          emptyLabel={t(($) => $.description_history.empty_side)}
        />
      </div>
    );
  }

  const rows: Array<{ key: string; line: DiffLine } | { key: string; skipped: number }> = [];
  let rendered = 0;
  let truncated = false;

  for (const [hunkIndex, hunk] of diff.hunks.entries()) {
    if (hunk.skippedBefore > 0) {
      rows.push({ key: `skip-${hunkIndex}`, skipped: hunk.skippedBefore });
    }
    for (const [lineIndex, line] of hunk.lines.entries()) {
      if (maxLines !== undefined && rendered >= maxLines) {
        truncated = true;
        break;
      }
      rows.push({ key: `${hunkIndex}-${lineIndex}`, line });
      rendered++;
    }
    if (truncated) break;
  }

  return (
    <div className={cn("overflow-hidden rounded-md border border-border bg-muted/30", className)}>
      <div className="overflow-x-auto">
        {rows.map((row) =>
          "skipped" in row ? (
            <div
              key={row.key}
              className="border-y border-border/60 bg-muted/50 px-2 py-0.5 text-[11px] text-muted-foreground"
            >
              {t(($) => $.description_history.skipped_lines, { count: row.skipped })}
            </div>
          ) : (
            <DiffRow key={row.key} line={row.line} />
          ),
        )}
      </div>
      {truncated && (
        <div className="border-t border-border/60 bg-muted/50 px-2 py-1 text-[11px] text-muted-foreground">
          {t(($) => $.description_history.diff_truncated)}
        </div>
      )}
    </div>
  );
}

function DiffRow({ line }: { line: DiffLine }) {
  const isAdded = line.type === "added";
  const isRemoved = line.type === "removed";

  return (
    <div
      className={cn(
        "flex items-start gap-2 px-2 font-mono text-[11.5px] leading-[1.7]",
        isAdded && "bg-emerald-500/10",
        isRemoved && "bg-rose-500/10",
      )}
    >
      {/* Line numbers are tabular so the gutter never reflows as digits change. */}
      <span className="w-8 shrink-0 select-none text-right tabular-nums text-muted-foreground/60">
        {line.oldNumber ?? ""}
      </span>
      <span className="w-8 shrink-0 select-none text-right tabular-nums text-muted-foreground/60">
        {line.newNumber ?? ""}
      </span>
      <span
        className={cn(
          "w-3 shrink-0 select-none",
          isAdded && "text-emerald-600 dark:text-emerald-400",
          isRemoved && "text-rose-600 dark:text-rose-400",
        )}
      >
        {isAdded ? "+" : isRemoved ? "−" : " "}
      </span>
      {/* pre-wrap, not nowrap: a description line is prose and can be long, and
          horizontal scrolling to read a sentence is worse than wrapping it. */}
      <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">
        {line.segments
          ? line.segments.map((segment, i) => (
              <span
                key={i}
                className={cn(
                  segment.changed && isAdded && "rounded-[2px] bg-emerald-500/25",
                  segment.changed && isRemoved && "rounded-[2px] bg-rose-500/25",
                )}
              >
                {segment.text}
              </span>
            ))
          : line.content || " "}
      </span>
    </div>
  );
}

function RewriteSide({
  label,
  content,
  tone,
  emptyLabel,
}: {
  label: string;
  content: string;
  tone: "added" | "removed";
  emptyLabel: string;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <div
        className={cn(
          "border-b border-border px-2 py-1 text-[11px] font-medium",
          tone === "added"
            ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
            : "bg-rose-500/10 text-rose-700 dark:text-rose-400",
        )}
      >
        {label}
      </div>
      <div className="max-h-64 overflow-y-auto px-2 py-1.5">
        {content ? (
          <pre className="whitespace-pre-wrap break-words font-mono text-[11.5px] leading-[1.7] text-foreground">
            {content}
          </pre>
        ) : (
          <p className="text-[11.5px] italic text-muted-foreground">{emptyLabel}</p>
        )}
      </div>
    </div>
  );
}
