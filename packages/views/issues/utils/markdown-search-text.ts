/**
 * The text a reader sees when `markdown` renders, for counting in-page find
 * matches in timeline comments that are not mounted (MUL-9538).
 *
 * Find matches inside one DOM text node at a time, so a query never spans
 * formatting: `foo **bar**` renders as the text nodes "foo " and "bar". This
 * keeps the same boundaries by writing a newline wherever rendering starts a
 * new text node (the find input is one line, so no query contains one), and
 * drops the syntax itself. It is a cheap approximation, not a Markdown parser:
 * a mounted comment's DOM stays the source of truth for painting matches.
 */
export function markdownSearchText(markdown: string): string {
  const cached = cache.get(markdown);
  if (cached !== undefined) return cached;
  const text = toSearchText(markdown);
  cache.set(markdown, text);
  if (cache.size > MAX_CACHED) cache.delete(cache.keys().next().value!);
  return text;
}

// Keyed by the content itself, so an edited comment never reads a stale entry.
const MAX_CACHED = 4_000;
const cache = new Map<string, string>();

const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const TABLE_DELIMITER = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;
const THEMATIC_BREAK = /^ {0,3}([-*_])(\s*\1){2,}\s*$/;
const ENTITIES: Record<string, string> = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " ", "#39": "'" };

function toSearchText(markdown: string): string {
  const out: string[] = [];
  let fence: string | null = null;
  for (const line of markdown.replace(/\r\n?/g, "\n").split("\n")) {
    const marker = FENCE.exec(line)?.[1];
    if (fence) {
      // Highlighted code stays one line per row; only the fences are syntax.
      if (marker && marker[0] === fence[0] && marker.length >= fence.length) fence = null;
      else out.push(line);
      continue;
    }
    if (marker) {
      fence = marker;
      continue;
    }
    if (TABLE_DELIMITER.test(line) && line.includes("-") && line.includes("|")) continue;
    if (THEMATIC_BREAK.test(line)) continue;
    out.push(inlineText(blockContent(line)));
  }
  return out.join("\n");
}

/** Drop the block markers that open a line: quotes, headings, list items, task boxes. */
function blockContent(line: string): string {
  return line
    .replace(/^\s*(>\s?)+/, "")
    .replace(/^\s{0,3}#{1,6}(\s+|$)/, "")
    .replace(/^\s*([-*+]|\d{1,9}[.)])\s+(\[[ xX]\]\s+)?/, "")
    .replace(/\s+#+\s*$/, "");
}

function inlineText(line: string): string {
  // Escaped punctuation is literal text; hold it out of the syntax rules.
  const escaped: string[] = [];
  return line
    .replace(/\\([!-/:-@[-`{-~])/g, (_, char: string) => `\uE000${escaped.push(char) - 1}\uE001`)
    // Images render as previews, not text.
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "\n")
    // Links and mentions render their label only.
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "\n$1\n")
    .replace(/<(https?:\/\/[^>\s]+)>/g, "\n$1\n")
    .replace(/<\/?[a-zA-Z][^>]*>/g, "\n")
    .replace(/(`+)([^`]|[^`][\s\S]*?[^`])\1(?!`)/g, (_, _ticks: string, code: string) => `\n${code}\n`)
    .replace(/\*\*|__|~~|==/g, "\n")
    // Single `*` / `_` emphasis delimiters sit against a word on one side only;
    // snake_case and spaced operators stay literal.
    .replace(/(^|[^\w*])[*_](?=[^\s*_])/g, "$1\n")
    .replace(/([^\s*_])[*_](?=$|[^\w*])/g, "$1\n")
    // Table cells are separate nodes.
    .replace(/\|/g, "\n")
    .replace(/&(amp|lt|gt|quot|apos|nbsp|#39);/g, (_, name: string) => ENTITIES[name] ?? "")
    .replace(/\uE000(\d+)\uE001/g, (_, index: string) => escaped[Number(index)] ?? "");
}
