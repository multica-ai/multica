// Trace Event Presenter — the pure readability layer for the execution
// transcript. Given one timeline event it decides visual kind, label, and
// one-line summary, encoding the reading hierarchy:
//
//   1. Agent text is the primary layer and reads without a click.
//   2. Errors stand out and also read without a click.
//   3. Tool calls are compact — provider-native name + most-informative arg.
//   4. Tool results and thinking are de-emphasized; their body opens in the
//      step inspector rather than in the list.
//   5. Unknown event types are retained as a generic event, never dropped.
//
// This module owns no React and no fetching, so it is unit-testable in
// isolation and independent of whichever list shell renders the events.

export interface TraceEvent {
  seq?: number;
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

/** Visual kind driving color/emphasis. `generic` covers any unknown `type`. */
export type TraceEventKind =
  | "agent"
  | "thinking"
  | "tool_use"
  | "tool_result"
  | "error"
  | "generic";

export function traceEventKind(event: TraceEvent): TraceEventKind {
  switch (event.type) {
    case "text":
      return "agent";
    case "thinking":
      return "thinking";
    case "tool_use":
      return "tool_use";
    case "tool_result":
      return "tool_result";
    case "error":
      return "error";
    default:
      return "generic";
  }
}

/**
 * Human label. Tool events show the provider-native tool name verbatim
 * (exec_command, patch_apply — never renamed); an unknown type shows its own
 * raw type string so evidence is never mislabeled.
 */
export function traceEventLabel(event: TraceEvent): string {
  switch (event.type) {
    case "text":
      return "Agent";
    case "thinking":
      return "Thinking";
    case "tool_use":
      return event.tool && event.tool.length > 0 ? event.tool : "Tool";
    case "tool_result":
      return event.tool && event.tool.length > 0 ? event.tool : "Result";
    case "error":
      return "Error";
    default:
      return event.type && event.type.length > 0 ? event.type : "Event";
  }
}

/** Shorten a long path to ".../parent/leaf" so a tool summary stays one line. */
export function shortenTracePath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

// Providers commonly wrap the real command in a login-shell invocation; the
// wrapper is pure noise in a one-line summary (the full original stays in the
// expanded params). Matches `<shell> -lc '<cmd>'` / `-c "<cmd>"` forms.
const SHELL_WRAPPER_PATTERN =
  /^(?:\/[\w./-]*\/)?(?:zsh|bash|sh|fish)\s+(?:-[a-z]+\s+)*(['"])([\s\S]+)\1$/;

export function stripShellWrapper(command: string): string {
  const match = SHELL_WRAPPER_PATTERN.exec(command.trim());
  return match?.[2] ?? command;
}

function clip(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) + "..." : value;
}

/**
 * Localizable phrasing the presenter cannot produce on its own. This module
 * stays free of React and i18n so it remains unit-testable in isolation, so the
 * caller injects the wording instead. Omitting it falls back to English, which
 * keeps the fallback safe rather than blank.
 */
export interface TraceSummaryLabels {
  /**
   * Phrase a multi-file patch, e.g. `src/a.go +2 more`.
   *
   * `extraCount` is the number of files *beyond* the named one, not the total.
   * Translations must say "and N more", not "N files in total" — the two read
   * almost the same in English and diverge in other languages.
   */
  morePaths?: (path: string, extraCount: number) => string;
}

/**
 * The single most informative argument of a tool call, as one line. Preference
 * order matches what a reviewer scans for first, falling back to the first
 * short string value.
 */
export function traceToolArgSummary(
  input: Record<string, unknown> | undefined,
  labels?: TraceSummaryLabels,
): string {
  if (!input) return "";
  const str = (v: unknown): string => (typeof v === "string" ? v : "");
  if (str(input.query)) return str(input.query);
  // A multi-file patch has no single path field; without this the row's
  // summary would fall through to the generic scan and come back empty.
  const patch = readPatchSummary(input, labels);
  if (patch) return patch;
  if (str(input.file_path)) return shortenTracePath(str(input.file_path));
  if (str(input.path)) return shortenTracePath(str(input.path));
  if (str(input.pattern)) return str(input.pattern);
  if (str(input.description)) return str(input.description);
  if (str(input.command)) return clip(stripShellWrapper(str(input.command)), 120);
  if (str(input.prompt)) return clip(str(input.prompt), 120);
  if (str(input.skill)) return str(input.skill);
  for (const v of Object.values(input)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

function firstLine(value: string | undefined): string {
  return value?.split("\n").find((l) => l.trim().length > 0) ?? "";
}

/**
 * Collapse all whitespace runs to single spaces. Unlike firstLine this keeps
 * content that spans lines, so a pretty-printed JSON result previews as
 * `[ { "id": ... } ]` instead of a lone opening bracket.
 */
function collapseWhitespace(value: string | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

/** One-line summary for the collapsed row — never contains a newline. */
export function traceEventSummary(event: TraceEvent, labels?: TraceSummaryLabels): string {
  switch (traceEventKind(event)) {
    case "thinking":
      return clip(firstLine(event.content), 200);
    case "tool_use":
      return traceToolArgSummary(event.input, labels);
    case "tool_result":
      // Unwrap first: the collapsed row is the one people read without
      // clicking, so it must not show transport escaping.
      return clip(collapseWhitespace(unwrapToolOutput(event.output ?? "")), 200);
    default:
      return firstLine(event.content ?? event.output);
  }
}

/**
 * Full, untruncated text for "copy all" — the complete body, not the one-line
 * summary. Tool calls copy their full input JSON; results and prose copy their
 * whole content. An RFC 3339 timestamp prefixes the line when the event has a
 * valid `created_at` (#5873). Callers apply secret redaction on the result.
 */
export function traceEventCopyText(event: TraceEvent): string {
  const label = traceEventLabel(event);
  let body: string;
  switch (traceEventKind(event)) {
    case "tool_use":
      body = event.input ? JSON.stringify(event.input, null, 2) : "";
      break;
    case "tool_result":
      // Match what the row displays, so copied evidence reads like the
      // terminal output rather than its transport encoding.
      body = unwrapToolOutput(event.output ?? "");
      break;
    default:
      body = event.content ?? "";
  }
  const date = event.created_at ? new Date(event.created_at) : null;
  const timestamp = date && !Number.isNaN(date.getTime()) ? `[${date.toISOString()}] ` : "";
  return body ? `${timestamp}[${label}] ${body}` : `${timestamp}[${label}]`;
}

/**
 * Tool output is persisted JSON-encoded, so a result arrives as a quoted string
 * whose newlines are escaped. Decode exactly one layer so it reads as the
 * terminal output it was. Anything that is not a wrapped string — a bare JSON
 * document, plain prose, a truncated body — is returned untouched.
 */
export function unwrapToolOutput(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed.length < 2 || !trimmed.startsWith('"') || !trimmed.endsWith('"')) return raw;
  try {
    const decoded: unknown = JSON.parse(trimmed);
    return typeof decoded === "string" ? decoded : raw;
  } catch {
    return raw;
  }
}

export type TraceDiffLineKind = "add" | "remove" | "context" | "gap";

export interface TraceDiffLine {
  kind: TraceDiffLineKind;
  text: string;
  /** Number of context lines a `gap` stands in for. Absent on other kinds. */
  hidden?: number;
}

/**
 * Expanded-row body. A replacement reads as a diff; a whole-file write reads as
 * plain content, because nothing was compared — marking all of it `+` adds
 * noise, not information. A patch carries one entry per file, since a single
 * Codex `patch_apply` routinely touches several. Everything else is text.
 */
export type TraceEventDetail =
  | { kind: "diff"; path: string; lines: TraceDiffLine[] }
  | { kind: "file"; path: string; text: string; lineCount: number }
  | { kind: "patch"; files: TracePatchFile[]; truncated: boolean }
  | { kind: "text"; text: string };

/** Body of one file inside a multi-file patch. */
export type TracePatchBody =
  | { kind: "diff"; lines: TraceDiffLine[] }
  | { kind: "file"; text: string; lineCount: number }
  /** Path and kind are known but the body was dropped by the size budget. */
  | { kind: "none" };

export interface TracePatchFile {
  path: string;
  /** `add` | `delete` | `update`, verbatim from the provider when reported. */
  changeKind?: string;
  /** Rename destination, when the change moved the file. */
  movePath?: string;
  /** True when this file's body was trimmed to fit the payload budget. */
  truncated?: boolean;
  body: TracePatchBody;
}

/** An empty body is zero lines, not one blank line — a pure deletion has no `+`. */
function toLines(value: string): string[] {
  return value.length === 0 ? [] : value.split("\n");
}

// Above this product the LCS table costs more than the readability is worth, so
// the change degrades to a plain replacement block instead of a minimal diff.
const MAX_DIFF_CELLS = 250_000;

/** Minimal line diff. Removals precede additions inside a change block. */
export function diffTraceLines(before: string[], after: string[]): TraceDiffLine[] {
  const n = before.length;
  const m = after.length;
  const out: TraceDiffLine[] = [];

  if (n * m > MAX_DIFF_CELLS) {
    for (const text of before) out.push({ kind: "remove", text });
    for (const text of after) out.push({ kind: "add", text });
    return out;
  }

  // Flat (n+1) x (m+1) table: lcs[i][j] is the longest common subsequence of
  // before[i:] and after[j:]. Typed-array cells stay `number` under
  // noUncheckedIndexedAccess, and one allocation beats n+1 of them.
  const width = m + 1;
  const lcs = new Int32Array((n + 1) * width);
  const at = (i: number, j: number): number => lcs[i * width + j] ?? 0;
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * width + j] =
        before[i] === after[j] ? at(i + 1, j + 1) + 1 : Math.max(at(i + 1, j), at(i, j + 1));
    }
  }

  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    const beforeLine = before[i] ?? "";
    const afterLine = after[j] ?? "";
    if (beforeLine === afterLine) {
      out.push({ kind: "context", text: beforeLine });
      i++;
      j++;
    } else if (at(i + 1, j) >= at(i, j + 1)) {
      out.push({ kind: "remove", text: beforeLine });
      i++;
    } else {
      out.push({ kind: "add", text: afterLine });
      j++;
    }
  }
  while (i < n) out.push({ kind: "remove", text: before[i++] ?? "" });
  while (j < m) out.push({ kind: "add", text: after[j++] ?? "" });
  return out;
}

/** Context lines kept either side of a change before a run is collapsed. */
const DIFF_CONTEXT_LINES = 3;

/**
 * Collapse long unchanged stretches into a single `gap` row. A replacement can
 * carry a large `old_string` for a one-line change; without this the change is
 * buried in context that never moved. Runs short enough that collapsing would
 * not save a line are left alone.
 */
export function collapseDiffContext(
  lines: readonly TraceDiffLine[],
  contextLines: number = DIFF_CONTEXT_LINES,
): TraceDiffLine[] {
  const out: TraceDiffLine[] = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (line === undefined) break;
    if (line.kind !== "context") {
      out.push(line);
      index++;
      continue;
    }

    let end = index;
    while (end < lines.length && lines[end]?.kind === "context") end++;
    const run = lines.slice(index, end);
    // A leading/trailing run only needs context on the side facing a change.
    const head = index === 0 ? 0 : contextLines;
    const tail = end === lines.length ? 0 : contextLines;

    if (run.length <= head + tail + 1) {
      out.push(...run);
    } else {
      out.push(...run.slice(0, head));
      out.push({ kind: "gap", text: "", hidden: run.length - head - tail });
      out.push(...run.slice(run.length - tail));
    }
    index = end;
  }
  return out;
}

/**
 * A file mutation is identified by the *shape* of its input, never by tool name:
 * providers call this Edit, patch_apply, str_replace, write_file… and the
 * presenter's contract is to keep provider-native names verbatim.
 */
type FileMutation =
  | { mode: "replace"; path: string; before: string[]; after: string[] }
  | { mode: "write"; path: string; content: string };

/**
 * Parse a ready-made unified diff into diff rows.
 *
 * Codex reports an updated file as a unified diff rather than a before/after
 * pair, so there is nothing to compare — recomputing a diff would mean first
 * reconstructing both sides from the diff itself. Hunk headers become `gap`
 * rows, which is exactly what they denote: skipped, unchanged content.
 */
export function parseUnifiedDiff(diff: string): TraceDiffLine[] {
  const raw = diff.split("\n");
  // `split` on a trailing newline yields a phantom final element; a genuinely
  // empty trailing context line would have been " ", not "".
  if (raw.length > 0 && raw[raw.length - 1] === "") raw.pop();

  const out: TraceDiffLine[] = [];
  // File headers only exist ahead of the first hunk. Past that point every
  // line belongs to the file, and "---"/"+++" are ordinary changed lines whose
  // content happens to start with a dash or plus — a Markdown rule, a nested
  // patch, a comment banner. Treating them as headers anywhere deleted real
  // content from the diff.
  let inHunk = false;
  for (const line of raw) {
    if (line.startsWith("@@")) {
      inHunk = true;
      out.push({ kind: "gap", text: line });
      continue;
    }
    if (!inHunk) {
      if (
        line.startsWith("diff --git") ||
        line.startsWith("index ") ||
        line.startsWith("--- ") ||
        line.startsWith("+++ ") ||
        line === "---" ||
        line === "+++"
      ) {
        continue;
      }
    }
    // "\ No newline at end of file" is metadata, not a line of the file: a
    // real line starting with a backslash carries a +/-/space prefix first.
    if (line.startsWith("\\")) continue;
    if (line.startsWith("+")) {
      out.push({ kind: "add", text: line.slice(1) });
      continue;
    }
    if (line.startsWith("-")) {
      out.push({ kind: "remove", text: line.slice(1) });
      continue;
    }
    if (line.startsWith(" ")) {
      out.push({ kind: "context", text: line.slice(1) });
      continue;
    }
    // Tolerate a context line that lost its leading space rather than dropping
    // content on the floor.
    out.push({ kind: "context", text: line });
  }
  return out;
}

/**
 * Read the normalized multi-file patch payload that the Codex adapter records:
 * `{ changes: [{ path, kind, diff?, content?, move_path? }], truncated? }`.
 *
 * Returns null for anything that is not this shape, so an unrecognised payload
 * falls back to pretty JSON instead of rendering as an empty patch.
 */
function readPatchChanges(input: Record<string, unknown>): TracePatchFile[] | null {
  if (!Array.isArray(input.changes)) return null;

  const files: TracePatchFile[] = [];
  for (const entry of input.changes) {
    if (typeof entry !== "object" || entry === null) continue;
    const rec = entry as Record<string, unknown>;
    const path = typeof rec.path === "string" ? rec.path : "";
    if (path.length === 0) continue;

    const file: TracePatchFile = { path, body: { kind: "none" } };
    if (typeof rec.kind === "string" && rec.kind.length > 0) file.changeKind = rec.kind;
    if (typeof rec.move_path === "string" && rec.move_path.length > 0) {
      file.movePath = rec.move_path;
    }
    if (rec.truncated === true) file.truncated = true;

    if (typeof rec.diff === "string" && rec.diff.length > 0) {
      file.body = { kind: "diff", lines: parseUnifiedDiff(rec.diff) };
    } else if (typeof rec.content === "string") {
      // An added file can legitimately be empty, so this keys on the field
      // being present rather than on the content being non-empty.
      if (file.changeKind === "delete") {
        // The legacy protocol reports a deletion as the whole outgoing file.
        // All-removals states that; a green "+N" would say the opposite.
        file.body = {
          kind: "diff",
          lines: toLines(rec.content).map((text) => ({ kind: "remove" as const, text })),
        };
      } else {
        file.body = {
          kind: "file",
          text: rec.content,
          lineCount: toLines(rec.content).length,
        };
      }
    }
    files.push(file);
  }

  return files.length > 0 ? files : null;
}

/** First path plus a count, for the collapsed one-line summary. */
function readPatchSummary(
  input: Record<string, unknown> | undefined,
  labels?: TraceSummaryLabels,
): string {
  if (!input) return "";
  const files = readPatchChanges(input);
  if (files === null) return "";
  const first = files[0];
  if (first === undefined) return "";
  const head = shortenTracePath(first.path);
  if (files.length === 1) return head;
  const extraCount = files.length - 1;
  return labels?.morePaths?.(head, extraCount) ?? `${head} +${extraCount} more`;
}

function readFileMutation(input: Record<string, unknown>): FileMutation | null {
  const str = (v: unknown): string | null => (typeof v === "string" ? v : null);
  const path = str(input.file_path) ?? str(input.path);
  if (path === null) return null;

  const oldString = str(input.old_string);
  const newString = str(input.new_string);
  if (oldString !== null && newString !== null) {
    return { mode: "replace", path, before: toLines(oldString), after: toLines(newString) };
  }

  // Keyed on `content`, not on "the before side is empty": an edit whose
  // old_string is empty is an insertion into an existing file, which still
  // reads best as a diff.
  const content = str(input.content);
  if (content !== null) return { mode: "write", path, content };

  return null;
}

/**
 * Structured body for the expanded row. Edits become a diff so a reviewer sees
 * what changed rather than two escaped string literals; results are unwrapped;
 * every other tool call falls back to pretty JSON.
 */
export function traceEventDetail(event: TraceEvent): TraceEventDetail {
  switch (traceEventKind(event)) {
    case "tool_use": {
      if (!event.input) return { kind: "text", text: "" };
      const patch = readPatchChanges(event.input);
      if (patch !== null) {
        return { kind: "patch", files: patch, truncated: event.input.truncated === true };
      }
      const mutation = readFileMutation(event.input);
      if (mutation?.mode === "replace") {
        return {
          kind: "diff",
          path: mutation.path,
          lines: collapseDiffContext(diffTraceLines(mutation.before, mutation.after)),
        };
      }
      if (mutation?.mode === "write") {
        return {
          kind: "file",
          path: mutation.path,
          text: mutation.content,
          lineCount: toLines(mutation.content).length,
        };
      }
      return { kind: "text", text: JSON.stringify(event.input, null, 2) };
    }
    case "tool_result":
      return { kind: "text", text: unwrapToolOutput(event.output ?? "") };
    default:
      return { kind: "text", text: event.content ?? "" };
  }
}

/** An image a tool returned, ready to render. */
export interface TraceImageResult {
  mediaType: string;
  base64: string;
}

/**
 * A screenshot tool answers with a base64 image block, which the transcript
 * used to print as a wall of base64 — the single worst thing on the old
 * surface. Recognise it so the reader gets the picture instead.
 *
 * Only JSON that already looks like an image block is parsed: an 8KB command
 * output should not pay for a `JSON.parse` on every render.
 */
export function readImageResult(output: string | undefined): TraceImageResult | null {
  if (!output) return null;
  const head = output.slice(0, 200);
  if (!head.includes('"image"') || !head.includes('"base64"')) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(output);
  } catch {
    return null;
  }

  const blocks = Array.isArray(parsed) ? parsed : [parsed];
  for (const block of blocks) {
    if (typeof block !== "object" || block === null) continue;
    const record = block as Record<string, unknown>;
    if (record.type !== "image") continue;
    const source = record.source;
    if (typeof source !== "object" || source === null) continue;
    const sourceRecord = source as Record<string, unknown>;
    const data = sourceRecord.data;
    if (typeof data !== "string" || data.length === 0) continue;
    const mediaType =
      typeof sourceRecord.media_type === "string" ? sourceRecord.media_type : "image/png";
    return { mediaType, base64: data };
  }

  return null;
}

/** Rough decoded size of a base64 payload, for a human-readable label. */
export function base64ByteLength(base64: string): number {
  const padding = base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((base64.length * 3) / 4) - padding);
}

export function traceEventHasDetail(event: TraceEvent): boolean {
  switch (traceEventKind(event)) {
    case "tool_use":
      return !!event.input && Object.keys(event.input).length > 0;
    case "tool_result":
      return !!event.output && event.output.length > 0;
    default:
      return !!event.content && event.content.length > 0;
  }
}

/** Whether a monospace face fits the collapsed summary (commands/output). */
export function traceEventSummaryIsMono(kind: TraceEventKind): boolean {
  return kind === "tool_use" || kind === "tool_result";
}

