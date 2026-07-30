/**
 * Line diff for issue descriptions.
 *
 * Lives in core, with no DOM or UI dependency, because three clients need the
 * same answer: web and desktop render it, mobile renders its own view over the
 * same data, and the Go backend computes matching +/- counts for the timeline
 * badge (server/internal/util/linediff.go). Both implementations use plain LCS
 * over lines so the counts they produce agree.
 *
 * Markdown is diffed as raw source, not as rendered HTML: it is line-oriented,
 * and a rendered diff is both harder to compute and harder to trust.
 */

export type DiffLineType = "context" | "added" | "removed";

/** A word-level run inside a changed line. */
export interface DiffSegment {
  text: string;
  changed: boolean;
}

export interface DiffLine {
  type: DiffLineType;
  content: string;
  /** 1-based line number on the old side, null for an added line. */
  oldNumber: number | null;
  /** 1-based line number on the new side, null for a removed line. */
  newNumber: number | null;
  /**
   * Word-level breakdown, present only when this line was paired 1:1 with its
   * counterpart in the same change block. Absent means "highlight the whole
   * line" — for a wholly rewritten line, per-word marks are noise.
   */
  segments?: DiffSegment[];
}

/** A run of changed lines plus its surrounding context. */
export interface DiffHunk {
  lines: DiffLine[];
  oldStart: number;
  newStart: number;
  /** Unchanged lines skipped between the previous hunk and this one. */
  skippedBefore: number;
}

export interface DescriptionDiff {
  hunks: DiffHunk[];
  added: number;
  removed: number;
  /**
   * True when the two sides share almost no lines. A unified diff of a whole
   * rewrite degenerates into "delete everything, add everything", which tells
   * the reader nothing — the UI switches to a side-by-side full read instead.
   * Agents rewriting a description wholesale make this the common case, not an
   * edge case.
   */
  isRewrite: boolean;
  /** Neither side has content. */
  isEmpty: boolean;
}

/**
 * Above this many lines on either side the quadratic LCS table is skipped and
 * the diff is reported as a rewrite. Descriptions are prose measured in tens of
 * lines; anything larger is a pasted dump where a line diff would not be read
 * anyway.
 */
const MAX_DIFF_LINES = 1200;

/** Unchanged lines kept on each side of a change run. */
const CONTEXT_LINES = 3;

/**
 * Below this ratio of common lines to total lines the change is treated as a
 * rewrite rather than an edit.
 */
const REWRITE_SIMILARITY_THRESHOLD = 0.2;

/**
 * Splits into lines, treating a trailing newline as a terminator rather than an
 * extra empty line. The editor adds and drops one freely and that must not read
 * as a change.
 */
export function splitLines(text: string): string[] {
  if (text === "") return [];
  const trimmed = text.endsWith("\n") ? text.slice(0, -1) : text;
  if (trimmed === "") return [""];
  return trimmed.split("\n");
}

/** Length of the longest common subsequence, O(min(n,m)) memory. */
function lcsLength(a: string[], b: string[]): number {
  if (a.length === 0 || b.length === 0) return 0;
  let prev = new Uint32Array(b.length + 1);
  let curr = new Uint32Array(b.length + 1);
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      if (a[i - 1] === b[j - 1]) {
        curr[j] = prev[j - 1]! + 1;
      } else {
        curr[j] = Math.max(prev[j]!, curr[j - 1]!);
      }
    }
    const swap = prev;
    prev = curr;
    curr = swap;
  }
  return prev[b.length]!;
}

/**
 * Added/removed line counts. Mirrors CountLineDiff in the Go backend so the
 * timeline badge and the client agree.
 */
export function countLineDiff(
  oldText: string,
  newText: string,
): { added: number; removed: number } {
  if (oldText === newText) return { added: 0, removed: 0 };
  const oldLines = splitLines(oldText);
  const newLines = splitLines(newText);
  const common = lcsLength(oldLines, newLines);
  return { added: newLines.length - common, removed: oldLines.length - common };
}

/**
 * Full LCS walk producing an interleaved line list. Needs the whole table to
 * backtrack, so it is bounded by MAX_DIFF_LINES; callers check that first.
 */
function walkLineDiff(oldLines: string[], newLines: string[]): DiffLine[] {
  const n = oldLines.length;
  const m = newLines.length;
  const width = m + 1;
  const table = new Uint32Array((n + 1) * width);

  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * width + j] =
        oldLines[i] === newLines[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!);
    }
  }

  const lines: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (oldLines[i] === newLines[j]) {
      lines.push({
        type: "context",
        content: oldLines[i]!,
        oldNumber: i + 1,
        newNumber: j + 1,
      });
      i++;
      j++;
    } else if (table[(i + 1) * width + j]! >= table[i * width + j + 1]!) {
      lines.push({
        type: "removed",
        content: oldLines[i]!,
        oldNumber: i + 1,
        newNumber: null,
      });
      i++;
    } else {
      lines.push({
        type: "added",
        content: newLines[j]!,
        oldNumber: null,
        newNumber: j + 1,
      });
      j++;
    }
  }
  while (i < n) {
    lines.push({ type: "removed", content: oldLines[i]!, oldNumber: i + 1, newNumber: null });
    i++;
  }
  while (j < m) {
    lines.push({ type: "added", content: newLines[j]!, oldNumber: null, newNumber: j + 1 });
    j++;
  }
  return lines;
}

/** Splits a line into words plus the whitespace between them. */
function tokenize(line: string): string[] {
  return line.match(/\s+|[^\s]+/g) ?? [];
}

/**
 * Word-level segments for one replaced line pair. Only applied when the two
 * lines are related enough that per-word marks help; a wholly different line
 * gets no segments and the renderer highlights it entirely.
 */
function wordSegments(
  oldLine: string,
  newLine: string,
): { removed: DiffSegment[]; added: DiffSegment[] } | null {
  const oldTokens = tokenize(oldLine);
  const newTokens = tokenize(newLine);
  if (oldTokens.length === 0 || newTokens.length === 0) return null;

  // Similarity is measured over words only. Counting the whitespace between
  // them would make any two same-shaped lines look related ("alpha beta gamma"
  // vs "zzz yyy xxx" share both spaces and nothing else), and word-highlighting
  // two unrelated lines is worse than highlighting neither.
  const oldWords = oldTokens.filter((t) => t.trim() !== "");
  const newWords = newTokens.filter((t) => t.trim() !== "");
  if (oldWords.length === 0 || newWords.length === 0) return null;
  const commonWords = lcsLength(oldWords, newWords);
  const similarity = (2 * commonWords) / (oldWords.length + newWords.length);
  if (similarity < 0.3) return null;

  const n = oldTokens.length;
  const m = newTokens.length;
  const width = m + 1;
  const table = new Uint32Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * width + j] =
        oldTokens[i] === newTokens[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!);
    }
  }

  const removed: DiffSegment[] = [];
  const added: DiffSegment[] = [];
  const push = (target: DiffSegment[], text: string, changed: boolean) => {
    const last = target[target.length - 1];
    if (last && last.changed === changed) {
      last.text += text;
      return;
    }
    target.push({ text, changed });
  };

  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (oldTokens[i] === newTokens[j]) {
      push(removed, oldTokens[i]!, false);
      push(added, newTokens[j]!, false);
      i++;
      j++;
    } else if (table[(i + 1) * width + j]! >= table[i * width + j + 1]!) {
      push(removed, oldTokens[i]!, true);
      i++;
    } else {
      push(added, newTokens[j]!, true);
      j++;
    }
  }
  while (i < n) push(removed, oldTokens[i++]!, true);
  while (j < m) push(added, newTokens[j++]!, true);

  return { removed, added };
}

/**
 * Attaches word-level segments to change blocks where removals and additions
 * pair up one-to-one. A block that removes 3 and adds 1 has no meaningful
 * pairing, so it is left alone.
 */
function annotateWordDiffs(lines: DiffLine[]): void {
  let index = 0;
  while (index < lines.length) {
    if (lines[index]!.type === "context") {
      index++;
      continue;
    }
    let end = index;
    while (end < lines.length && lines[end]!.type !== "context") end++;

    const block = lines.slice(index, end);
    const removals = block.filter((l) => l.type === "removed");
    const additions = block.filter((l) => l.type === "added");
    if (removals.length > 0 && removals.length === additions.length) {
      for (let k = 0; k < removals.length; k++) {
        const segments = wordSegments(removals[k]!.content, additions[k]!.content);
        if (segments) {
          removals[k]!.segments = segments.removed;
          additions[k]!.segments = segments.added;
        }
      }
    }
    index = end;
  }
}

/** Collapses long unchanged runs, keeping CONTEXT_LINES around each change. */
function buildHunks(lines: DiffLine[]): DiffHunk[] {
  const keep = new Array<boolean>(lines.length).fill(false);
  for (let i = 0; i < lines.length; i++) {
    if (lines[i]!.type === "context") continue;
    for (let k = Math.max(0, i - CONTEXT_LINES); k <= Math.min(lines.length - 1, i + CONTEXT_LINES); k++) {
      keep[k] = true;
    }
  }

  const hunks: DiffHunk[] = [];
  let current: DiffLine[] = [];
  let skipped = 0;
  let pendingSkip = 0;

  const flush = () => {
    if (current.length === 0) return;
    const first = current[0]!;
    hunks.push({
      lines: current,
      oldStart: first.oldNumber ?? 0,
      newStart: first.newNumber ?? 0,
      skippedBefore: pendingSkip,
    });
    current = [];
  };

  for (let i = 0; i < lines.length; i++) {
    if (keep[i]) {
      if (current.length === 0) {
        pendingSkip = skipped;
        skipped = 0;
      }
      current.push(lines[i]!);
    } else {
      flush();
      skipped++;
    }
  }
  flush();
  return hunks;
}

/**
 * Diffs two description snapshots.
 *
 * `isRewrite` callers should render a side-by-side full read rather than the
 * hunks — the hunks are still returned so a caller can offer both.
 */
export function diffDescriptions(oldText: string, newText: string): DescriptionDiff {
  const oldLines = splitLines(oldText);
  const newLines = splitLines(newText);

  if (oldLines.length === 0 && newLines.length === 0) {
    return { hunks: [], added: 0, removed: 0, isRewrite: false, isEmpty: true };
  }

  const oversized = oldLines.length > MAX_DIFF_LINES || newLines.length > MAX_DIFF_LINES;
  if (oversized) {
    return {
      hunks: [],
      added: newLines.length,
      removed: oldLines.length,
      isRewrite: true,
      isEmpty: false,
    };
  }

  const lines = walkLineDiff(oldLines, newLines);
  const added = lines.filter((l) => l.type === "added").length;
  const removed = lines.filter((l) => l.type === "removed").length;
  const common = lines.length - added - removed;
  const total = Math.max(oldLines.length, newLines.length);
  // An empty starting point is a first write, not a rewrite — there is nothing
  // the reader is losing, so the normal added-lines view reads correctly.
  const isRewrite =
    oldLines.length > 0 && newLines.length > 0 && total > 0 && common / total < REWRITE_SIMILARITY_THRESHOLD;

  annotateWordDiffs(lines);

  return {
    hunks: buildHunks(lines),
    added,
    removed,
    isRewrite,
    isEmpty: false,
  };
}
