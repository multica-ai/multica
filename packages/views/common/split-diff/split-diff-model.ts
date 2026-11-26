import { diffLines, diffWordsWithSpace, type Change } from "diff";
import type { TraceDiffLine } from "../task-transcript/trace-event-presenter";

export type SplitDiffKind = "equal" | "add" | "remove" | "empty";

export type SplitDiffPart = {
  text: string;
  type: "equal" | "add" | "remove";
};

export type SplitDiffCell = {
  kind: SplitDiffKind;
  text: string;
  parts: SplitDiffPart[];
};

export type SplitDiffRow = {
  left: SplitDiffCell;
  right: SplitDiffCell;
  leftLine: number | null;
  rightLine: number | null;
};

export type DiffHunkRange = {
  oldStart: number;
  oldCount: number;
  newStart: number;
  newCount: number;
};

export type DiffViewItem =
  | { type: "row"; row: SplitDiffRow }
  | {
      type: "collapse";
      id: string;
      hidden: SplitDiffRow[];
      hunk: DiffHunkRange;
      preview: string;
      trailing: boolean;
    }
  | {
      type: "hunk";
      id: string;
      hunk: DiffHunkRange;
      preview: string;
    };

export const DIFF_CONTEXT_LINES = 3;

/** Render-row count at which a transcript card starts as a placeholder. */
export const HUGE_DIFF_ROW_THRESHOLD = 2000;

/** Unified / source byte count at which a transcript card starts as a placeholder. */
export const HUGE_DIFF_BYTE_THRESHOLD = 200_000;

/** Container width below which split becomes a unified single column. */
export const SPLIT_UNIFIED_BREAKPOINT = 720;

/** Visible items before the split view opts into virtualization. */
export const VIRTUALIZE_MIN_ITEMS = 80;

export function isChangeRow(row: SplitDiffRow): boolean {
  return row.left.kind !== "equal" || row.right.kind !== "equal";
}

export function countDiffStats(rows: SplitDiffRow[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const row of rows) {
    if (row.right.kind === "add") added += 1;
    if (row.left.kind === "remove") removed += 1;
  }
  return { added, removed };
}

export function isHugeDiff(rowCount: number, bytes: number): boolean {
  return rowCount >= HUGE_DIFF_ROW_THRESHOLD || bytes >= HUGE_DIFF_BYTE_THRESHOLD;
}

export function isBinaryDiffText(text: string): boolean {
  if (text.includes("\0")) return true;
  if (/^GIT binary patch/m.test(text)) return true;
  if (/Binary files .+ differ/.test(text)) return true;
  return false;
}

const HUNK_HEADER = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$/;

export function parseHunkHeader(text: string): (DiffHunkRange & { preview: string }) | null {
  const match = HUNK_HEADER.exec(text.trim());
  if (!match) return null;
  return {
    oldStart: Number(match[1]),
    oldCount: match[2] === undefined ? 1 : Number(match[2]),
    newStart: Number(match[3]),
    newCount: match[4] === undefined ? 1 : Number(match[4]),
    preview: (match[5] ?? "").trim(),
  };
}

function changeToLines(value: string): string[] {
  if (value === "") return [];
  const lines = value.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
}

function partsFromChanges(changes: Change[], side: "remove" | "add"): SplitDiffPart[] {
  const parts: SplitDiffPart[] = [];
  for (const change of changes) {
    if (side === "remove" && change.added) continue;
    if (side === "add" && change.removed) continue;
    parts.push({
      text: change.value,
      type: change.added ? "add" : change.removed ? "remove" : "equal",
    });
  }
  return parts;
}

function cell(kind: SplitDiffKind, text: string, parts?: SplitDiffPart[]): SplitDiffCell {
  return {
    kind,
    text,
    parts: parts ?? [{ text, type: kind === "empty" ? "equal" : kind === "equal" ? "equal" : kind }],
  };
}

type DraftRow = { left: SplitDiffCell; right: SplitDiffCell };

function pairModifiedLine(before: string, after: string): DraftRow {
  const inline = diffWordsWithSpace(before, after);
  return {
    left: cell("remove", before, partsFromChanges(inline, "remove")),
    right: cell("add", after, partsFromChanges(inline, "add")),
  };
}

function pairRemovedAndAdded(removed: string[], added: string[]): DraftRow[] {
  const rows: DraftRow[] = [];
  const paired = Math.min(removed.length, added.length);
  for (let pair = 0; pair < paired; pair += 1) {
    rows.push(pairModifiedLine(removed[pair]!, added[pair]!));
  }
  for (let leftover = paired; leftover < removed.length; leftover += 1) {
    rows.push({
      left: cell("remove", removed[leftover]!),
      right: cell("empty", ""),
    });
  }
  for (let leftover = paired; leftover < added.length; leftover += 1) {
    rows.push({
      left: cell("empty", ""),
      right: cell("add", added[leftover]!),
    });
  }
  return rows;
}

export function buildSplitDiff(before: string, after: string): SplitDiffRow[] {
  const changes = diffLines(before, after);
  const rows: DraftRow[] = [];

  let index = 0;
  while (index < changes.length) {
    const change = changes[index];
    if (!change) break;

    if (!change.added && !change.removed) {
      for (const line of changeToLines(change.value)) {
        rows.push({
          left: cell("equal", line),
          right: cell("equal", line),
        });
      }
      index += 1;
      continue;
    }

    const removed: string[] = [];
    const added: string[] = [];
    while (index < changes.length && changes[index]?.removed) {
      removed.push(...changeToLines(changes[index]!.value));
      index += 1;
    }
    while (index < changes.length && changes[index]?.added) {
      added.push(...changeToLines(changes[index]!.value));
      index += 1;
    }
    rows.push(...pairRemovedAndAdded(removed, added));
  }

  if (rows.length === 0) {
    rows.push({
      left: cell("empty", ""),
      right: cell("empty", ""),
    });
  }

  return numberSplitRows(rows);
}

function numberSplitRows(rows: DraftRow[]): SplitDiffRow[] {
  let leftLine = 0;
  let rightLine = 0;
  return rows.map((row) => ({
    ...row,
    leftLine: row.left.kind === "empty" ? null : (leftLine += 1),
    rightLine: row.right.kind === "empty" ? null : (rightLine += 1),
  }));
}

/**
 * A whole-file write: empty before, every line an addition. Named so callers
 * do not reach for `buildSplitDiff("", unifiedPatch)` — that forges a file
 * out of hunk headers.
 */
export function splitDiffFromAddedFile(content: string): SplitDiffRow[] {
  return numberSplitRows(pairRemovedAndAdded([], changeToLines(content)));
}

function hunkRangeFor(rows: SplitDiffRow[]): DiffHunkRange {
  const oldLines = rows.filter((row) => row.left.kind !== "empty");
  const newLines = rows.filter((row) => row.right.kind !== "empty");
  return {
    oldStart: oldLines[0]?.leftLine ?? 1,
    oldCount: oldLines.length,
    newStart: newLines[0]?.rightLine ?? 1,
    newCount: newLines.length,
  };
}

function previewFor(rows: SplitDiffRow[]): string {
  const first = rows.find((row) => row.right.text || row.left.text);
  return (first?.right.text || first?.left.text || "").trim();
}

/** Hide long unchanged stretches, keeping `context` lines around each change. */
export function collapseUnchangedRows(
  rows: SplitDiffRow[],
  context = DIFF_CONTEXT_LINES,
): DiffViewItem[] {
  if (rows.length === 0) return [];

  const visible = Array.from({ length: rows.length }, () => false);
  const changeIndexes = rows
    .map((row, index) => (isChangeRow(row) ? index : -1))
    .filter((index) => index >= 0);

  if (changeIndexes.length === 0) {
    if (rows.length <= context * 2) {
      return rows.map((row) => ({ type: "row" as const, row }));
    }
    const hidden = rows.slice(context, rows.length - context);
    return [
      ...rows.slice(0, context).map((row) => ({ type: "row" as const, row })),
      {
        type: "collapse",
        id: `collapse-${context}`,
        hidden,
        hunk: hunkRangeFor(rows.slice(rows.length - context)),
        preview: previewFor(rows.slice(rows.length - context)),
        trailing: false,
      },
      ...rows.slice(rows.length - context).map((row) => ({ type: "row" as const, row })),
    ];
  }

  for (const index of changeIndexes) {
    const from = Math.max(0, index - context);
    const to = Math.min(rows.length - 1, index + context);
    for (let cursor = from; cursor <= to; cursor += 1) visible[cursor] = true;
  }

  const items: DiffViewItem[] = [];
  let cursor = 0;
  while (cursor < rows.length) {
    if (visible[cursor]) {
      items.push({ type: "row", row: rows[cursor]! });
      cursor += 1;
      continue;
    }
    const start = cursor;
    while (cursor < rows.length && !visible[cursor]) cursor += 1;
    const hidden = rows.slice(start, cursor);
    const nextVisible: SplitDiffRow[] = [];
    for (let peek = cursor; peek < rows.length && visible[peek]; peek += 1) {
      nextVisible.push(rows[peek]!);
    }
    const trailing = cursor >= rows.length;
    items.push({
      type: "collapse",
      id: `collapse-${start}`,
      hidden,
      hunk: hunkRangeFor(nextVisible.length > 0 ? nextVisible : hidden),
      preview: previewFor(nextVisible.length > 0 ? nextVisible : hidden),
      trailing,
    });
  }
  return items;
}

export function expandDiffViewItems(
  items: DiffViewItem[],
  expanded: ReadonlySet<string>,
): DiffViewItem[] {
  const rendered: DiffViewItem[] = [];
  for (const item of items) {
    if (item.type === "row" || item.type === "hunk") {
      rendered.push(item);
      continue;
    }
    if (expanded.has(item.id)) {
      for (const row of item.hidden) {
        rendered.push({ type: "row", row });
      }
      continue;
    }
    rendered.push(item);
  }
  return rendered;
}

/**
 * Turn already-parsed unified / hunk lines into split-diff rows.
 *
 * Line numbers come from `@@` headers. This must not be replaced with
 * `buildSplitDiff("", diffString)` — that treats hunk headers as added file
 * content.
 */
export function splitDiffFromTraceLines(lines: readonly TraceDiffLine[]): DiffViewItem[] {
  const items: DiffViewItem[] = [];
  const counters = { left: 0, right: 0 };
  let removed: string[] = [];
  let added: string[] = [];
  let hunkIndex = 0;

  const flushPair = () => {
    if (removed.length === 0 && added.length === 0) return;
    const drafts = pairRemovedAndAdded(removed, added);
    for (const draft of drafts) {
      const leftLine = draft.left.kind === "empty" ? null : (counters.left += 1);
      const rightLine = draft.right.kind === "empty" ? null : (counters.right += 1);
      items.push({ type: "row", row: { ...draft, leftLine, rightLine } });
    }
    removed = [];
    added = [];
  };

  for (const [index, line] of lines.entries()) {
    if (line.kind === "gap") {
      flushPair();
      const parsed = parseHunkHeader(line.text);
      if (parsed) {
        counters.left = parsed.oldStart - 1;
        counters.right = parsed.newStart - 1;
        items.push({
          type: "hunk",
          id: `hunk-${hunkIndex}-${index}`,
          hunk: parsed,
          preview: parsed.preview,
        });
        hunkIndex += 1;
      } else if ((line.hidden ?? 0) > 0) {
        const hidden = line.hidden ?? 0;
        items.push({
          type: "hunk",
          id: `gap-${index}`,
          hunk: {
            oldStart: counters.left + 1,
            oldCount: hidden,
            newStart: counters.right + 1,
            newCount: hidden,
          },
          preview: "",
        });
        counters.left += hidden;
        counters.right += hidden;
      }
      continue;
    }
    if (line.kind === "context") {
      flushPair();
      counters.left += 1;
      counters.right += 1;
      items.push({
        type: "row",
        row: {
          left: cell("equal", line.text),
          right: cell("equal", line.text),
          leftLine: counters.left,
          rightLine: counters.right,
        },
      });
      continue;
    }
    if (line.kind === "remove") {
      if (added.length > 0) flushPair();
      removed.push(line.text);
      continue;
    }
    added.push(line.text);
  }
  flushPair();
  return items;
}

export function countTraceDiffStats(lines: readonly TraceDiffLine[]): {
  added: number;
  removed: number;
  rows: number;
} {
  let added = 0;
  let removed = 0;
  let rows = 0;
  for (const line of lines) {
    if (line.kind === "gap") continue;
    rows += 1;
    if (line.kind === "add") added += 1;
    else if (line.kind === "remove") removed += 1;
  }
  return { added, removed, rows };
}
