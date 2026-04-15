import { preprocessLinks } from "@multica/ui/markdown";
import { preprocessMentionShortcodes } from "@multica/ui/markdown";
import { isFileCardUrl } from "../extensions/file-card";

/**
 * Preprocess a markdown string before loading into Tiptap via contentType: 'markdown'.
 *
 * This is the ONLY transform applied before @tiptap/markdown parses the content.
 * It does NOT convert to HTML — that was the old markdownToHtml.ts pipeline which
 * was deleted in the April 2026 refactor.
 *
 * Three string→string transforms on raw Markdown:
 * 1. Legacy mention shortcodes [@ id="..." label="..."] → [@Label](mention://member/id)
 *    (old serialization format in database, migrated on read)
 * 2. Raw URLs → markdown links via linkify-it (so they render as clickable Link nodes)
 * 3. CDN file links on their own line → HTML div for fileCard node parsing
 */
export function preprocessMarkdown(markdown: string): string {
  if (!markdown) return "";
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = preprocessLinks(step1);
  const step3 = preprocessFileCards(step2);
  return step3;
}

/**
 * LEGACY MIGRATION: Convert standalone `[name](cdnUrl)` lines into HTML that
 * Tiptap's fileCard parseHTML can recognise. Only matches non-image CDN URLs
 * on their own line.
 *
 * New file cards are saved as `!file[name](url)` via the fileCard extension's
 * markdownTokenizer, which is unambiguous and doesn't need this preprocessing.
 * This function remains for backward compatibility with content saved before
 * the `!file` syntax was introduced. As users edit old content, it auto-migrates
 * to the new syntax on save.
 *
 * Input:  `[report.pdf](https://multica-static.copilothub.ai/xxx.pdf)`
 * Output: `<div data-type="fileCard" data-href="url" data-filename="report.pdf"></div>`
 */
/** New syntax: !file[name](url) — unambiguous, no hostname matching needed. */
const NEW_FILE_CARD_RE = /^!file\[([^\]]*)\]\((https?:\/\/[^)]+)\)$/;

/** Legacy syntax: [name](cdnUrl) on its own line — matched by CDN hostname. */
const FILE_LINK_LINE = /^\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/;

function escapeAttr(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function toFileCardHtml(filename: string, url: string): string {
  return `<div data-type="fileCard" data-href="${escapeAttr(url)}" data-filename="${escapeAttr(filename)}"></div>`;
}

function preprocessFileCards(markdown: string): string {
  return markdown
    .split("\n")
    .map((line) => {
      const trimmed = line.trim();

      // New syntax: !file[name](url) — always a file card, no hostname check needed.
      const newMatch = trimmed.match(NEW_FILE_CARD_RE);
      if (newMatch) {
        return toFileCardHtml(newMatch[1]!, newMatch[2]!);
      }

      // Legacy: [name](cdnUrl) on its own line — CDN hostname matching.
      const match = trimmed.match(FILE_LINK_LINE);
      if (!match) return line;
      const filename = match[1]!;
      const url = match[2]!;
      if (!isFileCardUrl(url)) return line;
      return toFileCardHtml(filename, url);
    })
    .join("\n");
}
