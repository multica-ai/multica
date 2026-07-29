// Trace Event Presenter — the pure readability layer for the execution
// transcript. Given one timeline event it decides visual kind, label, one-line
// summary, and default expansion, encoding the reading hierarchy:
//
//   1. Agent text is the primary layer and reads without a click.
//   2. Errors stand out and also read without a click.
//   3. Tool calls are compact — provider-native name + most-informative arg.
//   4. Tool results and thinking are de-emphasized and collapsed by default.
//   5. Unknown event types are retained as a generic event, never dropped.
//
// This module owns no React and no fetching, so it is unit-testable in
// isolation and independent of whichever list shell renders the events.

import type { TranscriptDetailDensity } from "@multica/core/agents/stores";

export type { TranscriptDetailDensity };

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
 * The single most informative argument of a tool call, as one line. Preference
 * order matches what a reviewer scans for first, falling back to the first
 * short string value.
 */
export function traceToolArgSummary(input: Record<string, unknown> | undefined): string {
  if (!input) return "";
  const str = (v: unknown): string => (typeof v === "string" ? v : "");
  if (str(input.query)) return str(input.query);
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
export function traceEventSummary(event: TraceEvent): string {
  switch (traceEventKind(event)) {
    case "thinking":
      return clip(firstLine(event.content), 200);
    case "tool_use":
      return traceToolArgSummary(event.input);
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
 * noise, not information. Everything else is text.
 */
export type TraceEventDetail =
  | { kind: "diff"; path: string; lines: TraceDiffLine[] }
  | { kind: "file"; path: string; text: string; lineCount: number }
  | { kind: "text"; text: string };

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

/**
 * Default expansion under the `smart` density: the reading hierarchy itself.
 * Agent text and errors read without a click; process noise stays folded.
 */
export function traceEventDefaultExpanded(
  event: TraceEvent,
  density: TranscriptDetailDensity,
): boolean {
  if (!traceEventHasDetail(event)) return false;
  switch (density) {
    case "expanded":
      return true;
    case "collapsed":
      return false;
    case "smart": {
      const kind = traceEventKind(event);
      return kind === "agent" || kind === "error";
    }
  }
}
