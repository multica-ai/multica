// Trace Event Presenter (MUL-5122) — the pure readability layer for the
// Execution Log. Given one persisted event it decides label, compact summary,
// visual emphasis, collapse default and monospace, encoding the reading-hierarchy
// rules agreed for the narrowed scope (readable transcript, no data fetching):
//
//   1. Agent text is the primary layer; long text previews and expands.
//   2. Tool calls are compact — "provider-native name · most-informative arg".
//   3. Tool results are compact — a short output preview, full body on expand.
//   4. Errors stand out and stay their own kind.
//   5. Thinking is de-emphasized and collapsed by default.
//   6. Provider-native tool names are shown verbatim (exec_command, patch_apply)
//      — never renamed to Bash.
//   7. Unknown event types are retained as a generic event, never dropped.
//
// This module owns no React and no fetching, so it is unit-testable in isolation
// and reusable by whichever list/virtualization shell renders the events.

/** Minimal structural shape of a persisted event. Accepts both the typed
 *  `TimelineItem` and a raw message; `type` stays an open string so an unknown
 *  provider/server event kind is presented, not discarded. */
export interface TraceEvent {
  seq?: number;
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

/** Visual kind, driving color/emphasis. `generic` covers any unknown `type`. */
export type TraceEventKind =
  | "agent"
  | "thinking"
  | "tool_use"
  | "tool_result"
  | "error"
  | "generic";

/** Reading hierarchy: body is primary, tools/thinking secondary, meta tertiary. */
export type TraceEventEmphasis = "primary" | "secondary" | "tertiary";

/** Longest text shown before it collapses behind "expand"; adjacent streaming
 *  text is already merged upstream in `buildTimeline`. */
export const TRACE_TEXT_PREVIEW_LINES = 8;
/** Lines of a tool result shown inline before "expand". */
export const TRACE_RESULT_PREVIEW_LINES = 2;

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
 * Human label. Tool events show the provider-native tool name verbatim; a
 * missing tool falls back to a neutral word rather than an invented name. An
 * unknown type shows its own raw type string so evidence is never mislabeled.
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
      // Unknown/generic event — surface the raw type instead of hiding it.
      return event.type && event.type.length > 0 ? event.type : "Event";
  }
}

/** Shorten a long path to ".../parent/leaf" so a tool summary stays one line. */
export function shortenTracePath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

function clip(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) + "..." : value;
}

/**
 * The single most informative argument of a tool call, as a one-line string.
 * Preference order matches what a reviewer scans for first (command, then the
 * path/query/pattern), falling back to the first short string value.
 */
export function traceToolArgSummary(input: Record<string, unknown> | undefined): string {
  if (!input) return "";
  const inp = input as Record<string, unknown>;
  const str = (v: unknown): string => (typeof v === "string" ? v : "");
  if (str(inp.query)) return str(inp.query);
  if (str(inp.file_path)) return shortenTracePath(str(inp.file_path));
  if (str(inp.path)) return shortenTracePath(str(inp.path));
  if (str(inp.pattern)) return str(inp.pattern);
  if (str(inp.description)) return str(inp.description);
  if (str(inp.command)) return clip(str(inp.command), 120);
  if (str(inp.prompt)) return clip(str(inp.prompt), 120);
  if (str(inp.skill)) return str(inp.skill);
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

/** First non-empty line of a block of text. */
function firstLine(content: string | undefined): string {
  return content?.split("\n").find((l) => l.trim().length > 0) ?? "";
}

/**
 * Defensively decode a persisted `tool_result` output for display (MUL-5122).
 *
 * Some historical records (Claude/CodeBuddy history) were stored
 * double-JSON-encoded, so a shell log came back as `"...\n..."` — a JSON string
 * literal with outer quotes and escaped newlines rather than the raw text. This
 * unwraps exactly ONE level when the whole output is itself valid JSON, and
 * pretty-prints a JSON object/array, while leaving plain logs — and real
 * backslash paths like `C:\Users\x` — verbatim.
 *
 * Conservative by construction: only an output whose first non-space char is
 * `"`, `{`, or `[` is parsed at all, and a parse failure or a non-container
 * primitive returns the original text untouched. It never does a global
 * `\n`-replace, which would corrupt legitimate backslash sequences.
 */
export function decodeToolResultOutput(output: string): { text: string; json: boolean } {
  const head = output.trimStart()[0];
  if (head !== '"' && head !== "{" && head !== "[") {
    return { text: output, json: false };
  }
  try {
    const parsed: unknown = JSON.parse(output.trim());
    if (typeof parsed === "string") {
      // Historical double-encoding: unwrap one level to real newlines/quotes.
      return { text: parsed, json: false };
    }
    if (parsed !== null && typeof parsed === "object") {
      return { text: JSON.stringify(parsed, null, 2), json: true };
    }
    return { text: output, json: false };
  } catch {
    return { text: output, json: false };
  }
}

/** Priority keys a JSON tool-result summary surfaces first, so an object result
 *  reads as its identity (identifier / title / status …) rather than a bare `{`. */
const RESULT_SUMMARY_KEYS = [
  "identifier",
  "id",
  "key",
  "title",
  "name",
  "status",
  "state",
  "message",
  "error",
  "summary",
  "url",
];

/** A primitive field rendered as a scannable string, or null for objects/arrays. */
function primitiveField(value: unknown): string | null {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return null;
}

/** `key: value · key: value` from up to three of an object's most identifying
 *  primitive fields (priority keys first, then any remaining primitives). Falls
 *  back to the brace-wrapped key names when the object has no primitive field. */
function objectFieldSummary(obj: Record<string, unknown>): string {
  const parts: string[] = [];
  const used = new Set<string>();
  const push = (k: string): void => {
    const p = primitiveField(obj[k]);
    if (p !== null && p.trim().length > 0) {
      parts.push(`${k}: ${clip(p.trim(), 60)}`);
      used.add(k);
    }
  };
  for (const k of RESULT_SUMMARY_KEYS) {
    if (parts.length >= 3) break;
    if (k in obj) push(k);
  }
  for (const k of Object.keys(obj)) {
    if (parts.length >= 3) break;
    if (!used.has(k)) push(k);
  }
  if (parts.length > 0) return parts.join(" · ");
  const keys = Object.keys(obj);
  if (keys.length === 0) return "{}";
  return `{ ${keys.slice(0, 4).join(", ")}${keys.length > 4 ? ", …" : ""} }`;
}

/**
 * Compact one-line summary of a `tool_result` for the collapsed row and copy.
 * A JSON object/array result is summarized by its most identifying fields
 * (identifier / title / status …) rather than `firstLine` of the pretty JSON,
 * which for a pure object/array would be a useless `{` or `[` (MUL-5122).
 * Plain-text and historically double-encoded-string results fall back to their
 * first non-empty line.
 */
export function traceToolResultSummary(output: string): string {
  const decoded = decodeToolResultOutput(output);
  if (!decoded.json) return firstLine(decoded.text);
  try {
    const parsed: unknown = JSON.parse(output.trim());
    if (Array.isArray(parsed)) {
      const count = parsed.length;
      const first = parsed[0];
      let head = "";
      if (first !== null && typeof first === "object" && !Array.isArray(first)) {
        head = objectFieldSummary(first as Record<string, unknown>);
      } else if (count > 0) {
        head = parsed
          .slice(0, 3)
          .map(primitiveField)
          .filter((v): v is string => v !== null && v.trim().length > 0)
          .map((v) => clip(v.trim(), 40))
          .join(", ");
      }
      return head ? `[${count}] ${head}` : `[${count}]`;
    }
    if (parsed !== null && typeof parsed === "object") {
      return objectFieldSummary(parsed as Record<string, unknown>);
    }
  } catch {
    // fall through to the decoded first line
  }
  return firstLine(decoded.text);
}

/** The readable text a single row's copy action places on the clipboard: the
 *  decoded tool result, the tool input JSON, or the event's own content. */
export function traceEventCopyText(event: TraceEvent): string {
  switch (event.type) {
    case "tool_result":
      return decodeToolResultOutput(event.output ?? "").text;
    case "tool_use":
      return event.input ? JSON.stringify(event.input, null, 2) : "";
    case "text":
    case "thinking":
    case "error":
      return event.content ?? "";
    default:
      return (
        event.content ??
        event.output ??
        (event.input ? JSON.stringify(event.input, null, 2) : "")
      );
  }
}

/**
 * Compact one-line summary shown before any expansion. Tool calls surface their
 * argument; results and text surface a leading preview; errors surface their
 * content so failures are readable at a glance.
 */
export function traceEventSummary(event: TraceEvent): string {
  switch (event.type) {
    case "text":
      return firstLine(event.content);
    case "thinking":
      return event.content?.slice(0, 200) ?? "";
    case "tool_use":
      return traceToolArgSummary(event.input);
    case "tool_result":
      // Summarize from the DECODED output so the collapsed line and copy read
      // real text (double-encoded history) and JSON object/array results read as
      // their key fields, not a bare `{` / `[`.
      return traceToolResultSummary(event.output ?? "");
    case "error":
      return event.content ?? "";
    default:
      // Generic event: prefer content, then output, then a hint of the input.
      return (
        firstLine(event.content) ||
        (event.output?.slice(0, 200) ?? "") ||
        (event.input ? clip(JSON.stringify(event.input), 200) : "")
      );
  }
}

/** True when the event carries a body worth expanding to. */
export function traceEventHasDetail(event: TraceEvent): boolean {
  switch (event.type) {
    case "tool_use":
      return !!event.input && Object.keys(event.input).length > 0;
    case "tool_result":
      return !!event.output && event.output.length > 0;
    case "thinking":
    case "text":
    case "error":
      return !!event.content && event.content.length > 0;
    default:
      return (
        (!!event.content && event.content.length > 0) ||
        (!!event.output && event.output.length > 0) ||
        (!!event.input && Object.keys(event.input).length > 0)
      );
  }
}

/**
 * Reading hierarchy: agent text is the primary layer a reviewer reads; tool
 * calls, results and thinking are the secondary layer; anything else is
 * tertiary chrome.
 */
export function traceEventEmphasis(event: TraceEvent): TraceEventEmphasis {
  switch (traceEventKind(event)) {
    case "agent":
    case "error":
      return "primary";
    case "tool_use":
    case "tool_result":
    case "thinking":
      return "secondary";
    default:
      return "tertiary";
  }
}

/** Thinking is de-emphasized and starts collapsed; everything else starts open
 *  to its (bounded) preview. */
export function traceEventDefaultCollapsed(event: TraceEvent): boolean {
  return event.type === "thinking";
}

/** Command lines, tool input JSON and tool output render in a monospace block
 *  with preserved newlines. */
export function traceEventIsMonospace(event: TraceEvent): boolean {
  return event.type === "tool_use" || event.type === "tool_result";
}

/**
 * The filter key an event contributes to. A tool event keys by its tool so one
 * tool chip covers both its tool_use and tool_result; every other event keys by
 * type. Mirrors the server-side facet grouping so client and server filters agree.
 */
export function traceEventFilterKey(event: TraceEvent): string {
  return event.tool && (event.type === "tool_use" || event.type === "tool_result")
    ? `tool:${event.tool}`
    : event.type;
}
