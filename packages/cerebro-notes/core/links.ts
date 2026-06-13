// CEREBRO-PATCH(cerebro-notes-links): TECH-3421 — link a note to the work it is
// about (issue / channel / DM / chat). A note link is plain navigation, not a
// notification, so it intentionally does NOT go through the server mention
// parser (server/internal/util/mention.go) — it is stored inline in the note
// body as a standard markdown link with a `mention://` href and surfaced by the
// Notes UI as a clickable chip. This keeps the feature fully fork-clean: no
// schema change, no upstream mention coupling.
import type { WorkspacePaths } from "@multica/core/paths";

// The kinds of work a note can point at. "channel" and "dm" are issues with
// kind channel/dm; "chat" is an agent chat session; "issue" is a normal issue.
export type NoteLinkType = "issue" | "channel" | "dm" | "chat";

export const NOTE_LINK_TYPES: NoteLinkType[] = [
  "issue",
  "channel",
  "dm",
  "chat",
];

export interface NoteLink {
  type: NoteLinkType;
  id: string;
  /** Human label shown in the chip and inside the markdown link text. */
  label: string;
}

const LINK_TYPE_SET = new Set<string>(NOTE_LINK_TYPES);

// Matches a markdown link whose href is a note mention: [label](mention://type/id).
// The label may contain escaped brackets (\[ \]); ids are uuid-ish but we accept
// any non-slash, non-paren run so a malformed id never silently drops the chip.
const NOTE_LINK_RE =
  /\[((?:[^\]\\]|\\.)*)\]\(mention:\/\/(issue|channel|dm|chat)\/([^)\s/]+)\)/g;

function escapeLabel(label: string): string {
  // Keep the markdown link text balanced: escape the brackets that would
  // otherwise close the link early. Collapse newlines so a link stays on one
  // line inside the body.
  return label.replace(/[[\]]/g, (m) => `\\${m}`).replace(/\s+/g, " ").trim();
}

function unescapeLabel(label: string): string {
  return label.replace(/\\([[\]])/g, "$1");
}

/** Render a NoteLink as the markdown snippet inserted into the note body. */
export function noteLinkMarkdown(link: NoteLink): string {
  const label = escapeLabel(link.label) || link.id;
  return `[${label}](mention://${link.type}/${link.id})`;
}

/**
 * Parse every note link out of a body, in document order. Duplicates (same
 * type+id) are kept — the chip row dedupes for display so the same link added
 * twice in the text still shows once.
 */
export function parseNoteLinks(body: string): NoteLink[] {
  if (!body) return [];
  const out: NoteLink[] = [];
  NOTE_LINK_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = NOTE_LINK_RE.exec(body)) !== null) {
    const type = m[2] ?? "";
    if (!LINK_TYPE_SET.has(type)) continue;
    out.push({
      type: type as NoteLinkType,
      id: m[3] ?? "",
      label: unescapeLabel(m[1] ?? ""),
    });
  }
  return out;
}

/** Distinct links (by type+id), first occurrence wins — for the chip row. */
export function distinctNoteLinks(body: string): NoteLink[] {
  const seen = new Set<string>();
  const out: NoteLink[] = [];
  for (const link of parseNoteLinks(body)) {
    const key = `${link.type}/${link.id}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(link);
  }
  return out;
}

/**
 * Insert a link into the body at a caret offset. Adds a single leading space
 * when the preceding character is not already whitespace, and a trailing space
 * so the user keeps typing after the chip. Returns the new body plus the caret
 * position to restore after the inserted snippet.
 */
export function insertNoteLink(
  body: string,
  caret: number,
  link: NoteLink,
): { body: string; caret: number } {
  const pos = Math.max(0, Math.min(caret, body.length));
  const before = body.slice(0, pos);
  const after = body.slice(pos);
  const needsLeadingSpace = before.length > 0 && !/\s$/.test(before);
  const snippet = `${needsLeadingSpace ? " " : ""}${noteLinkMarkdown(link)} `;
  const nextCaret = pos + snippet.length;
  return { body: before + snippet + after, caret: nextCaret };
}

/** Resolve the in-app destination for a note link. */
export function noteLinkHref(paths: WorkspacePaths, link: NoteLink): string {
  switch (link.type) {
    case "issue":
      return paths.issueDetail(link.id);
    case "channel":
    case "dm":
      return paths.channelDetail(link.id);
    case "chat":
      return `${paths.inbox()}?chat=${encodeURIComponent(link.id)}`;
    default:
      return paths.inbox();
  }
}
