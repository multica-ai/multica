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
      return clip(collapseWhitespace(event.output), 200);
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
      body = event.output ?? "";
      break;
    default:
      body = event.content ?? "";
  }
  const date = event.created_at ? new Date(event.created_at) : null;
  const timestamp = date && !Number.isNaN(date.getTime()) ? `[${date.toISOString()}] ` : "";
  return body ? `${timestamp}[${label}] ${body}` : `${timestamp}[${label}]`;
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
