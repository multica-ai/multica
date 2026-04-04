import { preprocessLinks } from "@/components/markdown/linkify";
import { preprocessMentionShortcodes } from "@/components/markdown/mentions";

/**
 * Preprocess a markdown string before loading into Tiptap via contentType: 'markdown'.
 *
 * This is the ONLY transform applied before @tiptap/markdown parses the content.
 * It does NOT convert to HTML — that was the old markdownToHtml.ts pipeline which
 * was deleted in the April 2026 refactor.
 *
 * Two string→string transforms on raw Markdown:
 * 1. Legacy mention shortcodes [@ id="..." label="..."] → [@Label](mention://member/id)
 *    (old serialization format in database, migrated on read)
 * 2. Raw URLs → markdown links via linkify-it (so they render as clickable Link nodes)
 *
 * After this, @tiptap/markdown's parse() handles everything else: headings, lists,
 * tables, code blocks, and our custom mention tokenizer ([@Name](mention://type/id)).
 */
export function preprocessMarkdown(markdown: string): string {
  if (!markdown) return "";
  const step1 = preprocessMentionShortcodes(markdown);
  const step2 = preprocessLinks(step1);
  return step2;
}
