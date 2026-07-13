/**
 * Pure string transforms applied before marked.lexer parses the content.
 *
 * Two passes, both idempotent:
 *   1. Legacy mention shortcodes `[@ id="..." label="..."]` → modern
 *      mention link `[@Label](mention://member/id)`. Old DB rows from before
 *      the April 2026 migration use the shortcode form; the modern form is
 *      what marked.js can naturally tokenize as a markdown link. Calls into
 *      `@multica/core/markdown` (single source of truth — same regex web/
 *      desktop run).
 *
 *   2. File card lines `!file[name](url)` → standard link `[📎 name](url)`.
 *      marked.js doesn't recognize the `!file` prefix; web's preprocess
 *      turns it into HTML, which mobile can't render natively. Rewriting
 *      to a normal link with a 📎 emoji makes it a tappable link that
 *      `Linking.openURL` opens in the system viewer (Safari for PDFs,
 *      QuickLook for docs, share sheet for arbitrary files).
 *
 * NOTE: Web's preprocess also has a third pass that detects bare CDN
 * URLs as legacy file links. We skip that because mobile doesn't bootstrap
 * the cdnDomain config. Old comments using the legacy form render as plain
 * hyperlinks — same tap behavior, just no 📎 prefix. Acceptable degradation.
 */
import { preprocessMentionShortcodes } from "@multica/core/markdown";

// File-card line matcher, kept in sync with web's parser in
// `packages/ui/markdown/file-cards.ts` (NEW_FILE_CARD_RE + FILE_CARD_URL_PATTERN):
//
//   - Label allows backslash-escaped metacharacters (`\[ \] \\ \( \)`) so a
//     filename like `a]b.pdf` — which the CLI escapes to `a\]b.pdf` in its
//     `!file[...]` output (see cmd_attachment.go escapeMarkdownLabel) — is
//     captured whole. Backslash is excluded from the negated class so
//     overlapping alternatives can't backtrack (ReDoS, web #4881).
//   - URL is restricted to the same allowlist web accepts: site-relative
//     `/uploads/...` and `/api/attachments/<UUID>/download`, plus absolute
//     `http(s)://`. Anything else (`javascript:`, `data:`, `//host`, other
//     `/api/…`) is left as plain text so a stored card can't become an
//     out-of-band navigation.
const ATTACHMENT_UUID =
  "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}";
const FILE_CARD_URL = `/uploads/[^)]*|https?://[^)]+|/api/attachments/${ATTACHMENT_UUID}/download`;
const FILE_LINE_RE = new RegExp(
  `^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL})\\)$`,
);

// Unescape the file-card label back to the real filename (mirrors web's
// `newMatch[1].replace(/\\([[\]\\()])/g, "$1")`).
function unescapeFileLabel(label: string): string {
  return label.replace(/\\([[\]\\()])/g, "$1");
}

// Re-escape only the characters that would break a markdown LINK label, so the
// emitted `[📎 name](url)` stays valid markdown. Mobile's target is a link
// (re-parsed by marked / the enriched renderer), unlike web's HTML
// `data-filename` attribute — so a raw `]` must not truncate the link text.
function escapeLinkLabel(name: string): string {
  return name.replace(/([\\[\]])/g, "\\$1");
}

function preprocessFileCards(input: string): string {
  return input
    .split("\n")
    .map((line) => {
      const m = line.trim().match(FILE_LINE_RE);
      if (!m) return line;
      const label = escapeLinkLabel(unescapeFileLabel(m[1]!));
      return `[📎 ${label}](${m[2]})`;
    })
    .join("\n");
}

/**
 * Add GFM strikethrough markers around the content of checked task list items
 * so they render with `~~text~~` styling — matching Linear / Notion / Apple
 * Reminders / Things 3, where a checked item is visually crossed out.
 *
 * GFM itself does not specify that checked items SHOULD be struck through;
 * enriched-markdown's task-list renderer only changes the checkbox glyph and
 * (via `checkedTextColor`) dims the text. Without the strikethrough the
 * "done" state reads weakly, and users who expect the platform pattern from
 * other task apps assume the checkbox didn't take effect.
 *
 * Idempotent: skips lines whose body is already wrapped in `~~ ... ~~`.
 * Conservative regex — only matches `- [x]` / `* [x]` / `+ [x]` at the start
 * of a line (allowing leading whitespace), case-insensitive on the `x`.
 */
const TASK_DONE_RE = /^(\s*[-*+]\s+\[[xX]\]\s+)(.+)$/gm;

function preprocessTaskListStrikethrough(input: string): string {
  return input.replace(TASK_DONE_RE, (match, prefix, body) => {
    const trimmed = body.trim();
    if (trimmed.startsWith("~~") && trimmed.endsWith("~~")) return match;
    return `${prefix}~~${body}~~`;
  });
}

/**
 * Strip embedded HTML before marked sees it. Mobile cannot do what web does
 * (rehype-raw + sanitize → render real <br> / <sub> / <details>) — RN has
 * no inline HTML. Without this pass, users see literal `<br>` tags in the
 * comment body. Strategy:
 *
 *   - `<br>` / `<br/>` / `<br />` → `"  \n"` (two trailing spaces + newline,
 *     the canonical CommonMark hard-break syntax). md4c respects it as a
 *     hard line break inside a paragraph; bare `\n` would be treated as a
 *     space (CommonMark default), losing intentional `<br>` semantics.
 *   - HTML comments `<!-- ... -->` → removed entirely.
 *   - Every other tag → strip the tag, keep the inner text. So
 *     `<sub>2</sub>` becomes `2`. Loses formatting but keeps content; far
 *     better than showing raw HTML.
 *
 * Does not parse — pure regex. Cannot handle nested tags with attributes
 * containing `>`, but those don't appear in our editor output.
 */
function stripHtml(input: string): string {
  return input
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<br\s*\/?>/gi, "  \n")
    .replace(/<\/?[a-z][^>]*>/gi, "");
}

export function preprocessMobileMarkdown(input: string): string {
  if (!input) return "";
  return preprocessTaskListStrikethrough(
    preprocessFileCards(preprocessMentionShortcodes(stripHtml(input))),
  );
}
