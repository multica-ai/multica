/**
 * Unified diff parsing for the diff viewer (MUL-7651).
 *
 * Reads what `git diff` / `git format-patch` / `diff -u` write — the patches
 * the daemon captures for a run, the ones assembled from a pull request, and
 * `.patch` / `.diff` files people upload — into files, hunks and numbered
 * lines. Pure and platform-free: mobile can use it as-is.
 *
 * Hunks are read by their line counts, not by guessing from a line's first
 * character, so a removed line that itself starts with "--- " never ends a
 * file early.
 */

import type { CodeChangeFileStatus } from "../types/code-change";

export type DiffLineKind = "context" | "add" | "del";

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
  /** Line number on the old side; null for an added line. */
  oldNumber: number | null;
  /** Line number on the new side; null for a removed line. */
  newNumber: number | null;
  /** "\ No newline at end of file" followed this line. */
  noNewlineAtEnd?: boolean;
}

export interface DiffHunk {
  header: string;
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  /** The function or section git names after the second "@@". */
  section: string;
  lines: DiffLine[];
}

export interface ParsedDiffFile {
  path: string;
  /** Set when the file was renamed or copied from another path. */
  oldPath: string | null;
  status: CodeChangeFileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
  hunks: DiffHunk[];
}

const HUNK_HEADER = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$/;

/** Parse a unified diff into its files. Text outside any file is ignored. */
export function parsePatch(text: string): ParsedDiffFile[] {
  const files: ParsedDiffFile[] = [];
  const lines = text.split("\n");
  // A trailing newline leaves one empty element that is not a line.
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();

  let file: ParsedDiffFile | null = null;
  let gitHeader = false;
  let hunk: DiffHunk | null = null;
  let oldLeft = 0;
  let newLeft = 0;
  let oldNo = 0;
  let newNo = 0;

  const finishFile = () => {
    if (file) files.push(finalizeFile(file));
    file = null;
    hunk = null;
    gitHeader = false;
  };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]!;
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;

    if (hunk && (oldLeft > 0 || newLeft > 0)) {
      const target: DiffHunk = hunk;
      const marker = line[0];
      if (marker === "\\") {
        const last = target.lines[target.lines.length - 1];
        if (last) last.noNewlineAtEnd = true;
        continue;
      }
      if (marker === "+") {
        target.lines.push({ kind: "add", text: line.slice(1), oldNumber: null, newNumber: newNo++ });
        newLeft--;
        continue;
      }
      if (marker === "-") {
        target.lines.push({ kind: "del", text: line.slice(1), oldNumber: oldNo++, newNumber: null });
        oldLeft--;
        continue;
      }
      if (marker === " " || line === "") {
        // Some tools strip the space off an empty context line.
        target.lines.push({ kind: "context", text: line.slice(1), oldNumber: oldNo++, newNumber: newNo++ });
        oldLeft--;
        newLeft--;
        continue;
      }
      // Anything else ends a truncated hunk; read the line as a header.
      hunk = null;
    } else if (hunk && line.startsWith("\\")) {
      // "\ No newline at end of file" after the hunk's last counted line.
      const last = hunk.lines[hunk.lines.length - 1];
      if (last) last.noNewlineAtEnd = true;
      continue;
    }

    if (line.startsWith("diff --git ")) {
      finishFile();
      const [a, b] = splitGitHeaderPaths(line.slice("diff --git ".length));
      file = newFile(b ?? a ?? "", a !== null && b !== null && a !== b ? a : null);
      gitHeader = true;
      continue;
    }

    const hunkMatch = HUNK_HEADER.exec(line);
    if (hunkMatch && file) {
      const current: ParsedDiffFile = file;
      const next: DiffHunk = {
        header: line,
        oldStart: Number(hunkMatch[1]),
        oldLines: hunkMatch[2] === undefined ? 1 : Number(hunkMatch[2]),
        newStart: Number(hunkMatch[3]),
        newLines: hunkMatch[4] === undefined ? 1 : Number(hunkMatch[4]),
        section: hunkMatch[5]?.trim() ?? "",
        lines: [],
      };
      current.hunks.push(next);
      hunk = next;
      oldLeft = next.oldLines;
      newLeft = next.newLines;
      oldNo = next.oldStart;
      newNo = next.newStart;
      continue;
    }

    if (line.startsWith("--- ") && lines[i + 1]?.startsWith("+++ ")) {
      // A plain `diff -u` file has no "diff --git" line; its ---/+++ pair
      // opens it. Inside a git file the pair only names the two sides.
      if (!file || !gitHeader || (file as ParsedDiffFile).hunks.length > 0) {
        finishFile();
        file = newFile("", null);
      }
      const current: ParsedDiffFile = file!;
      const from = headerPath(line.slice(4));
      const to = headerPath(lines[i + 1]!.replace(/\r$/, "").slice(4));
      i++;
      if (from === null && to !== null) {
        current.status = "added";
        current.path = to;
      } else if (to === null && from !== null) {
        current.status = "deleted";
        current.path = from;
      } else if (from !== null && to !== null) {
        current.path = to;
        if (from !== to) current.oldPath = from;
      }
      continue;
    }

    if (!file) continue;
    const current: ParsedDiffFile = file;
    if (line.startsWith("new file mode")) current.status = "added";
    else if (line.startsWith("deleted file mode")) current.status = "deleted";
    else if (line.startsWith("rename from ")) {
      current.oldPath = unquotePath(line.slice("rename from ".length));
      current.status = "renamed";
    } else if (line.startsWith("rename to ")) {
      current.path = unquotePath(line.slice("rename to ".length));
      current.status = "renamed";
    } else if (line.startsWith("copy from ")) {
      current.oldPath = unquotePath(line.slice("copy from ".length));
      current.status = "copied";
    } else if (line.startsWith("copy to ")) {
      current.path = unquotePath(line.slice("copy to ".length));
      current.status = "copied";
    } else if (line.startsWith("Binary files ") || line === "GIT binary patch") {
      current.binary = true;
    }
  }
  finishFile();
  return files;
}

function newFile(path: string, oldPath: string | null): ParsedDiffFile {
  return {
    path,
    oldPath,
    status: oldPath ? "renamed" : "modified",
    additions: 0,
    deletions: 0,
    binary: false,
    hunks: [],
  };
}

function finalizeFile(file: ParsedDiffFile): ParsedDiffFile {
  let additions = 0;
  let deletions = 0;
  for (const hunk of file.hunks) {
    for (const line of hunk.lines) {
      if (line.kind === "add") additions++;
      else if (line.kind === "del") deletions++;
    }
  }
  if (file.oldPath === file.path) file.oldPath = null;
  if (file.status === "renamed" && !file.oldPath) file.status = "modified";
  return { ...file, additions, deletions };
}

// "a/x b/y" — split where both halves agree when there was no rename, which
// is what makes a path containing " b/" unambiguous in practice.
function splitGitHeaderPaths(rest: string): [string | null, string | null] {
  if (rest.startsWith('"')) {
    const first = readQuoted(rest);
    if (first) {
      const remainder = rest.slice(first.length).trimStart();
      const second = remainder.startsWith('"') ? unquotePath(remainder) : remainder;
      return [stripPrefix(unquotePath(first)), stripPrefix(second)];
    }
  }
  const candidates: number[] = [];
  for (let at = rest.indexOf(" b/"); at >= 0; at = rest.indexOf(" b/", at + 1)) candidates.push(at);
  for (const at of candidates) {
    const a = stripPrefix(rest.slice(0, at));
    const b = stripPrefix(rest.slice(at + 1));
    if (a === b) return [a, b];
  }
  if (candidates.length > 0) {
    const at = candidates[0]!;
    return [stripPrefix(rest.slice(0, at)), stripPrefix(rest.slice(at + 1))];
  }
  return [stripPrefix(rest), null];
}

function readQuoted(s: string): string | null {
  for (let i = 1; i < s.length; i++) {
    if (s[i] === "\\") {
      i++;
      continue;
    }
    if (s[i] === '"') return s.slice(0, i + 1);
  }
  return null;
}

function stripPrefix(path: string): string {
  return path.replace(/^[ab]\//, "");
}

// The path on a ---/+++ line: null for /dev/null. `diff -u` appends a tab and
// a timestamp.
function headerPath(value: string): string | null {
  let path = value.startsWith('"') ? unquotePath(value) : value.split("\t")[0]!;
  if (path === "/dev/null") return null;
  path = stripPrefix(path);
  return path;
}

/** Undo git's C-style path quoting ("\303\251" → "é"). Unquoted input passes through. */
export function unquotePath(value: string): string {
  const quoted = readQuoted(value);
  if (!value.startsWith('"') || !quoted) return value;
  const body = quoted.slice(1, -1);
  const bytes: number[] = [];
  for (let i = 0; i < body.length; i++) {
    const ch = body[i]!;
    if (ch !== "\\") {
      for (const b of new TextEncoder().encode(ch)) bytes.push(b);
      continue;
    }
    const next = body[++i];
    if (next === undefined) break;
    if (/[0-7]/.test(next)) {
      const octal = body.slice(i, i + 3);
      bytes.push(parseInt(octal, 8));
      i += octal.length - 1;
      continue;
    }
    const escapes: Record<string, number> = { n: 10, t: 9, r: 13, a: 7, b: 8, f: 12, v: 11, '"': 34, "\\": 92 };
    bytes.push(escapes[next] ?? next.charCodeAt(0));
  }
  return new TextDecoder().decode(new Uint8Array(bytes));
}
