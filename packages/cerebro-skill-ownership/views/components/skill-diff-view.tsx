"use client";

// Unified diff view for SKILL.md content (prose, not code). Modified lines
// get word-level highlighting instead of whole-line red/green blocks — the
// thing that makes a diff readable for edited sentences rather than just
// inserted/deleted ones (see FIR-2684). Long unchanged runs collapse into
// expandable dividers so a reviewer isn't scrolling past the whole file.

import { useState } from "react";
import { ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { computeDiff, groupForDisplay, type DiffLine, type WordSegment } from "./skill-diff";

interface Props {
  base: string;
  proposed: string;
  baseLabel?: string;
  proposedLabel?: string;
}

function renderWords(words: WordSegment[], type: "added" | "removed") {
  return words.map((seg, i) => (
    <span
      key={i}
      className={
        seg.changed
          ? type === "added"
            ? "rounded-[2px] bg-emerald-500/25 text-emerald-800 underline decoration-emerald-600 decoration-2 dark:text-emerald-300"
            : "rounded-[2px] bg-destructive/20 text-destructive line-through decoration-destructive/70"
          : undefined
      }
    >
      {seg.text}
    </span>
  ));
}

export function SkillDiffView({ base, proposed, baseLabel = "base", proposedLabel = "proposed" }: Props) {
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [expandAll, setExpandAll] = useState(false);

  const diff = computeDiff(base, proposed);
  const hasChanges = diff.some((l) => l.type !== "unchanged");

  if (!hasChanges) {
    return (
      <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        No differences between {baseLabel} and {proposedLabel}.
      </div>
    );
  }

  const added = diff.filter((l) => l.type === "added").length;
  const removed = diff.filter((l) => l.type === "removed").length;
  const groups = groupForDisplay(diff);
  const hasCollapsible = groups.some((g) => g.kind === "collapsed");

  const toggleGroup = (key: number) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  const renderLine = (line: DiffLine) => (
    <div
      className={
        line.type === "added"
          ? "flex bg-emerald-500/10"
          : line.type === "removed"
            ? "flex bg-destructive/10"
            : "flex text-muted-foreground"
      }
    >
      <span className="w-9 shrink-0 select-none pr-2 text-right font-mono text-[11px] text-muted-foreground/50 tabular-nums">
        {line.oldLine ?? ""}
      </span>
      <span className="w-9 shrink-0 select-none pr-2 text-right font-mono text-[11px] text-muted-foreground/50 tabular-nums">
        {line.newLine ?? ""}
      </span>
      <span className="mr-2 shrink-0 select-none font-mono">
        {line.type === "added" ? "+" : line.type === "removed" ? "−" : " "}
      </span>
      <span
        className={`whitespace-pre-wrap break-words pr-2 ${
          line.type === "added" ? "text-emerald-800 dark:text-emerald-300" : line.type === "removed" ? "text-destructive" : ""
        }`}
      >
        {line.type !== "unchanged" && line.words ? renderWords(line.words, line.type) : line.text || " "}
      </span>
    </div>
  );

  return (
    <div className="overflow-hidden rounded-md border bg-muted/20 text-sm leading-6">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/40 px-3 py-1.5 font-mono text-xs">
        <div className="flex items-center gap-3 text-muted-foreground">
          <span className="text-destructive/70">− {baseLabel}</span>
          <span className="text-emerald-600">+ {proposedLabel}</span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-emerald-600">+{added}</span>
          <span className="text-destructive/70">−{removed}</span>
          {hasCollapsible && (
            <button
              type="button"
              onClick={() => setExpandAll((v) => !v)}
              className="flex items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              {expandAll ? (
                <ChevronsDownUp className="h-3 w-3" />
              ) : (
                <ChevronsUpDown className="h-3 w-3" />
              )}
              {expandAll ? "Collapse context" : "Expand unchanged"}
            </button>
          )}
        </div>
      </div>
      <div>
        {groups.map((group) => {
          if (group.kind === "line") {
            return <div key={group.key}>{renderLine(group.line)}</div>;
          }
          const isExpanded = expandAll || expanded.has(group.key);
          if (isExpanded) {
            return (
              <div key={group.key}>
                {group.lines.map((l, i) => (
                  <div key={i}>{renderLine(l)}</div>
                ))}
              </div>
            );
          }
          return (
            <button
              key={group.key}
              type="button"
              onClick={() => toggleGroup(group.key)}
              className="flex w-full items-center gap-1.5 border-y border-dashed bg-muted/40 px-3 py-1 font-mono text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              <ChevronsUpDown className="h-3 w-3 shrink-0" />
              {group.lines.length} unchanged line{group.lines.length === 1 ? "" : "s"}
            </button>
          );
        })}
      </div>
    </div>
  );
}
