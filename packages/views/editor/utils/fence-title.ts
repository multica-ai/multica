/**
 * The `title` attribute of a fence info string — ```html title="Weekly p95"
 * (MUL-7649). The read-only renderers read it; the editable block's title bar
 * reads and writes it (MUL-9760).
 */

const TITLE_RE = /(?:^|\s)title=(?:"([^"]*)"|'([^']*)'|(\S+))/;

/** The title in a fence's meta (the info string after the language). Double, single or no quotes. */
export function parseFenceTitle(meta: string | null | undefined): string | null {
  if (!meta) return null;
  const match = TITLE_RE.exec(meta);
  const title = (match?.[1] ?? match?.[2] ?? match?.[3] ?? "").trim();
  return title || null;
}

function quoteTitle(title: string): string {
  if (!title.includes('"')) return `"${title}"`;
  if (!title.includes("'")) return `'${title}'`;
  // The attribute has no escape for a quote; a title with both kinds keeps one.
  return `"${title.replace(/"/g, "'")}"`;
}

/**
 * `meta` with its title set to `title`, or without a title when `title` is
 * blank. The rest of the info string stays exactly as written, so attributes
 * other renderers read survive a rename.
 */
export function withFenceTitle(meta: string | null | undefined, title: string): string | null {
  // A backtick ends the info string of a backtick fence and a line break ends
  // the fence line; either would break the block when it is saved.
  const clean = title.replace(/`/g, "").replace(/\s+/g, " ").trim();
  const attribute = clean ? `title=${quoteTitle(clean)}` : "";
  const current = meta ?? "";
  const match = TITLE_RE.exec(current);
  let next: string;
  if (match) {
    const lead = match[0].startsWith("title=") ? "" : match[0].charAt(0);
    next =
      current.slice(0, match.index) +
      (attribute ? lead + attribute : "") +
      current.slice(match.index + match[0].length);
  } else {
    next = attribute ? `${current} ${attribute}` : current;
  }
  return next.trim() || null;
}
