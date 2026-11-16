/**
 * Split Markdown into native prose, React code blocks, and interactive images.
 * Use lexer token boundaries so fenced/indented code and paragraph images are
 * handled without fragile regex reconstruction. Preserve prose through token.raw.
 *
 * Only top-level code is extracted. Nested list/quote code stays in the native
 * renderer to preserve its surrounding Markdown structure.
 */
import { marked, type Tokens } from "marked";
import { fromMarkdown } from "mdast-util-from-markdown";
import { normalizeUri } from "micromark-util-sanitize-uri";

function imageUrisBySource(input: string): Map<string, string> {
  // Marked's href has already lost backslash escapes. Read destinations from
  // the source with the sequence's CommonMark parser instead of decoding href
  // again: \&amp; must stay literal, while &amp; must decode once.
  const tree = fromMarkdown(input);
  type Node = typeof tree | (typeof tree.children)[number];
  const pending: Node[] = [tree];
  const nodes: Node[] = [];
  while (pending.length) {
    const node = pending.pop()!;
    nodes.push(node);
    if ("children" in node) {
      for (let i = node.children.length - 1; i >= 0; i--) pending.push(node.children[i]!);
    }
  }
  const definitions = new Map<string, string>();
  for (const node of nodes) {
    if (node.type === "definition" && !definitions.has(node.identifier)) {
      definitions.set(node.identifier, node.url);
    }
  }
  const uris = new Map<string, string>();
  for (const node of nodes) {
    if (node.type !== "image" && node.type !== "imageReference") continue;
    const url = node.type === "image" ? node.url : definitions.get(node.identifier);
    const start = node.position?.start.offset;
    const end = node.position?.end.offset;
    if (url !== undefined && start !== undefined && end !== undefined) {
      // Marked normalizes CRLF and bare CR before creating token.raw.
      const source = input.slice(start, end).replace(/\r\n?/g, "\n");
      uris.set(source, normalizeUri(url));
    }
  }
  return uris;
}

export type MarkdownSegment =
  | { type: "prose"; content: string }
  | { type: "code"; lang: string | undefined; code: string }
  | { type: "image"; uri: string; alt: string };

export function splitMarkdown(input: string): MarkdownSegment[] {
  if (!input) return [];

  const tokens = marked.lexer(input);
  const out: MarkdownSegment[] = [];
  let proseBuffer = "";
  let imageUris: Map<string, string> | undefined;

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
            // Only parse CommonMark destinations when an interactive image is
            // present. Marked-only extensions retain their lexer destination.
            uri: (imageUris ??= imageUrisBySource(input)).get(img.raw) ?? normalizeUri(img.href),
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
