/**
 * Mention URL serialization and parsing.
 *
 * The canonical mention URL shape is `mention://<type>/<id>[?ws=<wsId>]`.
 * The optional `?ws=` qualifier identifies a cross-workspace entity; it is
 * parsed by the backend at task-prep (KTD5) so the agent never crosses
 * the workspace boundary.
 *
 * All consumers — suggestion factory, editor, readonly renderer — should use
 * these helpers rather than string-concatenating URLs.
 */

import type { MentionType } from "./types";

/** Input for building a mention URL. */
export interface MentionUrlInput {
  type: MentionType | string;
  id: string;
  /** Optional workspace UUID for cross-workspace mentions. */
  ws?: string;
}

/**
 * Builds the markdown link href for a mention.
 * Returns `mention://<type>/<id>` for same-workspace,
 * or `mention://<type>/<id>?ws=<wsId>` for cross-workspace.
 */
export function buildMentionUrl(input: MentionUrlInput): string {
  const base = `mention://${input.type}/${input.id}`;
  if (input.ws) {
    return `${base}?ws=${input.ws}`;
  }
  return base;
}

/** Parsed mention URL components. */
export interface ParsedMentionUrl {
  type: string;
  id: string;
  ws?: string;
}

/**
 * Parses a mention:// href into its components.
 * Returns null if the URL is not a valid mention URL.
 */
export function parseMentionUrl(href: string): ParsedMentionUrl | null {
  if (!href.startsWith("mention://")) return null;

  const suffix = href.slice("mention://".length);
  // Split off optional ?ws= qualifier
  const qIndex = suffix.indexOf("?ws=");
  let idPart: string;
  let ws: string | undefined;
  if (qIndex !== -1) {
    idPart = suffix.slice(0, qIndex);
    ws = suffix.slice(qIndex + "?ws=".length);
  } else {
    idPart = suffix;
  }

  // idPart is "<type>/<id>"
  const slashIndex = idPart.indexOf("/");
  if (slashIndex === -1) return null;
  const type = idPart.slice(0, slashIndex);
  const id = idPart.slice(slashIndex + 1);
  if (!type || !id) return null;

  return { type, id, ws };
}
