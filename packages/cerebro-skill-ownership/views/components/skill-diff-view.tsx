"use client";

// Unified-diff-style view for SKILL.md content, collapsing long unchanged
// runs into expandable dividers so a reviewer sees only what changed (plus a
// little context) instead of scrolling through the whole file.

import { useState } from "react";
import { ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { computeDiff, groupForDisplay, type DiffLine } from "./skill-diff";

interface Props {
  base: string;
  proposed: string;
  baseLabel?: string;
  proposedLabel?: string;
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
          ? "flex bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
          : line.type === "removed"
            ? "flex bg-destructive/10 text-destructive/80"
            : "flex text-muted-foreground"
      }
    >
      <span className="w-9 shrink-0 select-none pr-2 text-right text-muted-foreground/50 tabular-nums">
        {line.oldLine ?? ""}
      </span>
      <span className="w-9 shrink-0 select-none pr-2 text-right text-muted-foreground/50 tabular-nums">
        {line.newLine ?? ""}
      </span>
      <span className="mr-2 shrink-0 select-none">
        {line.type === "added" ? "+" : line.type === "removed" ? "−" : " "}
      </span>
      <span className="whitespace-pre-wrap break-all pr-2">{line.text || " "}</span>
    </div>
  );

  return (
    <div className="overflow-hidden rounded-md border bg-muted/20 font-mono text-xs leading-5">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/40 px-3 py-1.5 text-xs">
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
              className="flex w-full items-center gap-1.5 border-y border-dashed bg-muted/40 px-3 py-1 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
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
