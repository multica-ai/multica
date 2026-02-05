/**
 * Exec Allowlist — Persistent command pattern matching and management
 *
 * Allowlist entries use glob-like patterns to match against commands.
 * Patterns are matched against the full command string or binary name.
 */

import { v7 as uuidv7 } from "uuid";
import type { ExecAllowlistEntry } from "./exec-approval-types.js";

/**
 * Match a command against allowlist entries.
 * Returns the first matching entry, or null if no match.
 *
 * Matching rules:
 * - Patterns are case-insensitive
 * - "*" matches any sequence of non-space characters (within a segment)
 * - "**" matches any sequence (including spaces)
 * - Exact match on the full command or command prefix
 * - Pattern "git *" matches "git status", "git log", etc.
 */
export function matchAllowlist(
  entries: ExecAllowlistEntry[],
  command: string,
): ExecAllowlistEntry | null {
  const normalizedCommand = command.trim().toLowerCase();
  if (!normalizedCommand) return null;

  for (const entry of entries) {
    if (matchPattern(entry.pattern, normalizedCommand)) {
      return entry;
    }
  }

  return null;
}

/**
 * Match a glob-like pattern against a command string.
 */
function matchPattern(pattern: string, command: string): boolean {
  const normalizedPattern = pattern.trim().toLowerCase();
  if (!normalizedPattern) return false;

  // Convert glob pattern to regex
  let regexStr = "^";
  let i = 0;
  while (i < normalizedPattern.length) {
    const ch = normalizedPattern[i]!;

    if (ch === "*") {
      if (normalizedPattern[i + 1] === "*") {
        // ** matches anything (including spaces)
        regexStr += ".*";
        i += 2;
      } else {
        // * matches non-space characters
        regexStr += "[^\\s]*";
        i += 1;
      }
    } else if (ch === "?") {
      regexStr += "[^\\s]";
      i += 1;
    } else {
      // Escape regex special characters
      regexStr += ch.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      i += 1;
    }
  }
  regexStr += "$";

  try {
    return new RegExp(regexStr).test(command);
  } catch {
    // Fallback to exact match if regex is invalid
    return normalizedPattern === command;
  }
}

/**
 * Add an entry to the allowlist.
 * Deduplicates by pattern (case-insensitive).
 * Returns the updated entries array.
 */
export function addAllowlistEntry(
  entries: ExecAllowlistEntry[],
  pattern: string,
): ExecAllowlistEntry[] {
  const normalizedPattern = pattern.trim().toLowerCase();

  // Check for duplicate
  const existing = entries.find(
    (e) => e.pattern.trim().toLowerCase() === normalizedPattern,
  );
  if (existing) return entries;

  const newEntry: ExecAllowlistEntry = {
    id: uuidv7(),
    pattern: pattern.trim(),
    lastUsedAt: Date.now(),
  };

  return [...entries, newEntry];
}

/**
 * Record usage of an allowlist entry.
 * Updates lastUsedAt and lastUsedCommand.
 * Returns the updated entries array.
 */
export function recordAllowlistUse(
  entries: ExecAllowlistEntry[],
  entry: ExecAllowlistEntry,
  command: string,
): ExecAllowlistEntry[] {
  return entries.map((e) => {
    if (e === entry || (e.id && e.id === entry.id) || e.pattern === entry.pattern) {
      return {
        ...e,
        lastUsedAt: Date.now(),
        lastUsedCommand: command,
      };
    }
    return e;
  });
}

/**
 * Remove an allowlist entry by pattern or ID.
 * Returns the updated entries array.
 */
export function removeAllowlistEntry(
  entries: ExecAllowlistEntry[],
  patternOrId: string,
): ExecAllowlistEntry[] {
  const normalized = patternOrId.trim().toLowerCase();
  return entries.filter(
    (e) =>
      e.pattern.trim().toLowerCase() !== normalized &&
      e.id !== patternOrId,
  );
}

/**
 * Normalize allowlist entries: assign missing IDs, deduplicate.
 */
export function normalizeAllowlist(
  entries: ExecAllowlistEntry[],
): ExecAllowlistEntry[] {
  const seen = new Set<string>();
  const result: ExecAllowlistEntry[] = [];

  for (const entry of entries) {
    const key = entry.pattern.trim().toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);

    result.push({
      ...entry,
      id: entry.id ?? uuidv7(),
    });
  }

  return result;
}
