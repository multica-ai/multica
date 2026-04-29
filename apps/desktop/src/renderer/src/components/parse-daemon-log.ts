// Pure parser for daemon log lines. The daemon writes via Go's slog with
// the `tint` handler in NoColor mode (the file isn't a TTY), so each line
// has a stable shape:
//
//   HH:MM:SS.mmm  LEVEL  message text  key=value key2="quoted value"
//
// We split it into structured pieces so the UI can render timestamp,
// level, message and structured fields in separate columns and let users
// filter / search across them. Anything that doesn't match (panic stack
// traces, third-party prints, partial writes during log rotation) falls
// back to a raw view — we never drop input.

export type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR";

export interface ParsedLogLine {
  /** Monotonic id assigned at receive time; stable across re-renders. */
  id: number;
  /** "HH:MM:SS.mmm" or null when the line didn't match the standard shape. */
  timestamp: string | null;
  level: LogLevel | null;
  /** Human-readable message body, with structured fields stripped off. */
  message: string;
  /** key/value pairs trailing the message. Empty if there were none. */
  fields: Record<string, string>;
  /** The original line, kept for fallback rendering and copy-to-clipboard. */
  raw: string;
}

// `tint` v1.x emits the 3-letter short form (DBG / INF / WRN / ERR) and,
// for non-standard slog levels, appends a signed delta (e.g. "INF+1",
// "DBG-2"). We accept both the short and 4-letter long forms (defensive
// against future config changes) and normalize them to a canonical
// 4-letter LogLevel. The optional `[+-]\d+` suffix is captured into the
// regex and discarded — surfacing `INF+1` to the UI doesn't help users
// and complicates the level filter chips.
const HEADER_RE =
  /^(\d{2}:\d{2}:\d{2}\.\d{3})\s+(DEBUG|DBG|INFO|INF|WARN|WRN|ERROR|ERR)(?:[+-]\d+)?\s+(.+)$/;

const LEVEL_NORMALIZE: Record<string, LogLevel> = {
  DEBUG: "DEBUG",
  DBG: "DEBUG",
  INFO: "INFO",
  INF: "INFO",
  WARN: "WARN",
  WRN: "WARN",
  ERROR: "ERROR",
  ERR: "ERROR",
};
// Anchored to the END of the remaining string so we peel one field at a
// time from the right. `value` is either a double-quoted string (which may
// contain escaped chars) or any non-whitespace run.
const TRAILING_FIELD_RE = /\s+([a-zA-Z_][a-zA-Z0-9_.]*)=("(?:[^"\\]|\\.)*"|\S+)$/;

function unquote(value: string): string {
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
    return value.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, "\\");
  }
  return value;
}

function extractTrailingFields(rest: string): {
  message: string;
  fields: Record<string, string>;
} {
  const fields: Record<string, string> = {};
  let work = rest;
  while (true) {
    const match = work.match(TRAILING_FIELD_RE);
    if (!match || match.index === undefined) break;
    fields[match[1]!] = unquote(match[2]!);
    work = work.slice(0, match.index);
  }
  return { message: work.trim(), fields };
}

export function parseLogLine(raw: string, id: number): ParsedLogLine {
  const match = raw.match(HEADER_RE);
  if (!match) {
    return { id, timestamp: null, level: null, message: raw, fields: {}, raw };
  }
  const [, timestamp, level, rest] = match;
  const normalized = LEVEL_NORMALIZE[level!];
  if (!normalized) {
    // Unknown level token — keep raw shape so we don't mis-categorize.
    return { id, timestamp: null, level: null, message: raw, fields: {}, raw };
  }
  const { message, fields } = extractTrailingFields(rest!);
  return {
    id,
    timestamp: timestamp!,
    level: normalized,
    message,
    fields,
    raw,
  };
}
