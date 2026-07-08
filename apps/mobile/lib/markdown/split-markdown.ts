/**
 * Hybrid-render segment splitter.
 *
 * react-native-enriched-markdown gives us native-quality rendering of
 * prose (paragraphs / headings / lists / quotes / tables / inline code /
 * links / mentions) but does NOT let us inject React for any leaf node
 * (see issues #54, #232, #246 — maintainer's stance is "no custom
 * renderers, ever, by design"). That blocks three product needs:
 *
 *   1. Shiki syntax-highlighted code blocks (web parity).
 *   2. Tap-to-lightbox on images.
 *   3. Horizontal scroll + copy button on code.
 *
 * Maintainer-endorsed workaround (issue #246): split the markdown at
 * those boundaries, render each fragment as its own enriched instance
 * for prose and as React for the rest. This file is that splitter.
 *
 * Strategy:
 *
 *   - Run `marked.lexer` (we already depend on it for preprocess).
 *   - Walk the top-level token list.
 *   - `code` token → emit a `code` segment.
 *   - `paragraph` token containing image tokens → split the paragraph
 *     at each image boundary. Each image becomes its own `image`
 *     segment; the surrounding text rejoins the prose stream. (RN's
 *     <Image> can't be inline in a <Text>, and tap-to-lightbox needs a
 *     Pressable wrapper that's not viable inside an attributed string
 *     anyway. GitHub mobile and Linear iOS both promote inline images
 *     to block-level — we follow.)
 *   - Every other block stays in the prose buffer and is handed to
 *     enriched verbatim via `token.raw`.
 *
 * Why not regex: code inside lists, indented code, paragraph-embedded
 * images, and consecutive code blocks without a blank line all have
 * boundaries regex can't reliably detect. marked.lexer already knows;
 * reuse it.
 *
 * Known trade-off: code blocks nested inside a list item stay with the
 * list and render via enriched (no Shiki). That's because the splitter
 * only inspects top-level tokens — descending into list/quote children
 * would force us to also re-serialise their surrounding markdown,
 * complicating prose reconstruction. Top-level code is the >95% case
 * for issue descriptions / comments; revisit if real content shows
 * otherwise.
 */
import { marked, type Tokens } from "marked";

export type MarkdownSegment =
  | { type: "prose"; content: string }
  | { type: "code"; lang: string | undefined; code: string }
  | { type: "image"; uri: string; alt: string };

export function splitMarkdown(input: string): MarkdownSegment[] {
  if (!input) return [];

  const tokens = marked.lexer(input);
  const out: MarkdownSegment[] = [];
  let proseBuffer = "";

  const flushProse = () => {
    const trimmed = proseBuffer.replace(/^\s+|\s+$/g, "");
    if (trimmed.length > 0) {
      out.push({ type: "prose", content: trimmed });
    }
    proseBuffer = "";
  };

  for (const token of tokens) {
    if (token.type === "code") {
      flushProse();
      const t = token as Tokens.Code;
      out.push({
        type: "code",
        lang: t.lang ? t.lang : undefined,
        code: t.text,
      });
      continue;
    }

    if (token.type === "paragraph") {
      const para = token as Tokens.Paragraph;
      const inline = para.tokens ?? [];
      const hasImage = inline.some((t) => t.type === "image");

      if (!hasImage) {
        proseBuffer += para.raw;
        continue;
      }

      // Split paragraph at each image. Text fragments before/after each
      // image rejoin the prose buffer as their own short paragraph.
      let textBuffer = "";
      const flushText = () => {
        const trimmed = textBuffer.trim();
        if (trimmed.length > 0) {
          // Append as its own paragraph so enriched treats consecutive
          // fragments as separate blocks rather than running them together.
          proseBuffer += trimmed + "\n\n";
        }
        textBuffer = "";
      };

      for (const t of inline) {
        if (t.type === "image") {
          flushText();
          flushProse();
          const img = t as Tokens.Image;
          out.push({
            type: "image",
            uri: img.href,
            alt: img.text ?? "",
          });
        } else {
          textBuffer += (t as { raw?: string }).raw ?? "";
        }
      }
      flushText();
      continue;
    }

    // Every other top-level block (heading, list, blockquote, table,
    // hr, html, space, def, …) goes to prose verbatim via token.raw.
    proseBuffer += (token as { raw?: string }).raw ?? "";
  }

  flushProse();
  return out;
}
