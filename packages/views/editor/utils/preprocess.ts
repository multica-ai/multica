import { preprocessLinks, preprocessMentionShortcodes, preprocessFileCards } from "@multica/ui/markdown";
import { configStore } from "@multica/core/config";

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
 * 3. File card syntax (new !file[name](url) + legacy [name](cdnUrl)) → HTML div for
 *    fileCard node parsing
 */
export function preprocessMarkdown(markdown: string): string {
  if (!markdown) return "";
  const cdnDomain = configStore.getState().cdnDomain;
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = preprocessLinks(step1);
  const step3 = preprocessFileCards(step2, cdnDomain);
  return step3;
}
