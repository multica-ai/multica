import Mention from "@tiptap/extension-mention";
import { mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { MentionView } from "./mention-view";

/**
 * Extended Mention extension with:
 * - Custom `type` attribute (member / agent / issue)
 * - ReactNodeViewRenderer for rich inline rendering
 * - Custom markdown format: [@Label](mention://type/id)
 *
 * Markdown strategy:
 * The parent @tiptap/extension-mention v3 ships with createInlineMarkdownSpec
 * that uses shortcode format [@ id="..." label="..."]. We OVERRIDE all three
 * markdown properties (markdownTokenizer, parseMarkdown, renderMarkdown) so
 * that our custom link-like format is used instead. The tokenizer also handles
 * the parent's shortcode format as a fallback for robustness.
 *
 * IMPORTANT: markdownTokenName must be set explicitly to match the tokenizer
 * name, ensuring MarkdownManager registers the correct parse handler.
 */
export const BaseMentionExtension = Mention.extend({
  // Explicit token name so MarkdownManager maps our parseMarkdown correctly
  markdownTokenName: "mention",

  addNodeView() {
    return ReactNodeViewRenderer(MentionView);
  },
  renderHTML({ node, HTMLAttributes }) {
    const type = node.attrs.type ?? "member";
    const prefix = type === "issue" ? "" : "@";
    return [
      "span",
      mergeAttributes(
        { "data-type": "mention" },
        this.options.HTMLAttributes,
        HTMLAttributes,
        {
          "data-mention-type": node.attrs.type ?? "member",
          "data-mention-id": node.attrs.id,
        },
      ),
      `${prefix}${node.attrs.label ?? node.attrs.id}`,
    ];
  },
  addAttributes() {
    return {
      ...this.parent?.(),
      type: {
        default: "member",
        parseHTML: (el: HTMLElement) =>
          el.getAttribute("data-mention-type") ?? "member",
        renderHTML: () => ({}),
      },
    };
  },

  // ---------------------------------------------------------------------------
  // Markdown: tokenizer, parser, serializer
  //
  // Primary format:   [@Label](mention://type/id)
  // Fallback parse:   [@ id="..." label="..." type="..."]  (parent shortcode)
  // ---------------------------------------------------------------------------

  markdownTokenizer: {
    name: "mention",
    level: "inline" as const,
    start(src: string) {
      // Match our link-like format first, then parent's shortcode as fallback
      const linkIdx = src.search(/\[@?[^\]]+\]\(mention:\/\//);
      const shortcodeIdx = src.search(/\[@\s+/);
      if (linkIdx === -1) return shortcodeIdx;
      if (shortcodeIdx === -1) return linkIdx;
      return Math.min(linkIdx, shortcodeIdx);
    },
    tokenize(src: string) {
      // Try link-like format: [@Label](mention://type/id)
      const linkMatch = src.match(
        /^\[@?([^\]]+)\]\(mention:\/\/(\w+)\/([^)]+)\)/,
      );
      if (linkMatch) {
        return {
          type: "mention",
          raw: linkMatch[0],
          attributes: {
            label: linkMatch[1],
            type: linkMatch[2] ?? "member",
            id: linkMatch[3],
          },
        };
      }
      // Fallback: parent's shortcode format [@ id="..." label="..."]
      const shortcodeMatch = src.match(/^\[@\s+([^\]]*)\]/);
      if (shortcodeMatch) {
        const attrs: Record<string, string> = {};
        const re = /(\w+)="([^"]*)"/g;
        let m;
        while ((m = re.exec(shortcodeMatch[1]!)) !== null) {
          if (m[1] && m[2] !== undefined) attrs[m[1]] = m[2];
        }
        if (attrs.id) {
          return {
            type: "mention",
            raw: shortcodeMatch[0],
            attributes: {
              id: attrs.id,
              label: attrs.label ?? attrs.id,
              type: attrs.type ?? "member",
            },
          };
        }
      }
      return undefined;
    },
  },

  parseMarkdown(token: any, helpers: any) {
    return helpers.createNode("mention", token.attributes);
  },

  renderMarkdown(node: any) {
    const { id, label, type = "member" } = node.attrs || {};
    const prefix = type === "issue" ? "" : "@";
    return `[${prefix}${label ?? id}](mention://${type}/${id})`;
  },
});
