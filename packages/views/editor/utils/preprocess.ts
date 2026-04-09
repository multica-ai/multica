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
 * Convert standalone `[name](cdnUrl)` lines into HTML that Tiptap's fileCard
 * parseHTML can recognise. Only matches non-image CDN URLs on their own line.
 *
 * Input:  `[report.pdf](https://multica-static.copilothub.ai/xxx.pdf)`
 * Output: `<div data-type="fileCard" data-href="url" data-filename="report.pdf"></div>`
 */
const FILE_LINK_LINE = /^\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/;

function preprocessFileCards(markdown: string): string {
  return markdown
    .split("\n")
    .map((line) => {
      const trimmed = line.trim();
      const match = trimmed.match(FILE_LINK_LINE);
      if (!match) return line;
      const filename = match[1]!;
      const url = match[2]!;
      if (!isFileCardUrl(url)) return line;
      return `<div data-type="fileCard" data-href="${url}" data-filename="${filename}"></div>`;
    })
    .join("\n");
}
