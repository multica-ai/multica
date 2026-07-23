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
      // real text, not an escaped `"...\n..."` blob from double-encoded history.
      return firstLine(decodeToolResultOutput(event.output ?? "").text);
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
