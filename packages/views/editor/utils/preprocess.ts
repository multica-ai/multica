import { preprocessLinks, preprocessMentionShortcodes, preprocessFileCards } from "@multica/ui/markdown";
import { configStore } from "@multica/core/config";

/**
 * Decode line-leading `&gt;` entities back to literal `>`.
 *
 * Why: @tiptap/markdown's text serializer calls `encodeHtmlEntities` on
 * paragraph text (@tiptap/core), turning any `>` at the start of a line into
 * `&gt;`. When that serialized markdown is later parsed by remark/marked, the
 * entity no longer triggers blockquote parsing, so a user-typed quote shows
 * up as a literal `>` character instead of a real blockquote.
 *
 * We only touch `&gt;` that appears at the very start of a line (optionally
 * after whitespace) so inline `&gt;` in prose (e.g. "2 &gt; 1") is left
 * alone. This restores blockquote rendering without affecting other content.
 */
function decodeLeadingBlockquoteEntities(markdown: string): string {
  return markdown.replace(/^([ \t]*)&gt;/gm, "$1>");
}

/**
 * Preprocess a markdown string before loading into Tiptap via contentType: 'markdown'.
 *
 * This is the ONLY transform applied before @tiptap/markdown parses the content.
 * It does NOT convert to HTML — that was the old markdownToHtml.ts pipeline which
 * was deleted in the April 2026 refactor.
 *
 * Four string→string transforms on raw Markdown:
 * 1. Legacy mention shortcodes [@ id="..." label="..."] → [@Label](mention://member/id)
 *    (old serialization format in database, migrated on read)
 * 2. Raw URLs → markdown links via linkify-it (so they render as clickable Link nodes)
 * 3. File card syntax (new !file[name](url) + legacy [name](cdnUrl)) → HTML div for
 *    fileCard node parsing
 * 4. Decode `&gt;` at the start of a line back to `>` so Tiptap-serialized
 *    blockquotes render as real blockquotes (see decodeLeadingBlockquoteEntities).
 */
export function preprocessMarkdown(markdown: string): string {
  if (!markdown) return "";
  const cdnDomain = configStore.getState().cdnDomain;
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = preprocessLinks(step1);
  const step3 = preprocessFileCards(step2, cdnDomain);
  const step4 = decodeLeadingBlockquoteEntities(step3);
  return step4;
}
