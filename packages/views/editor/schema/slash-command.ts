/**
 * slashCommand node schema and Markdown codec, without the React NodeView, so
 * the collaboration server can parse and serialize documents in Node.
 */
import Mention from "@tiptap/extension-mention";
import { mergeAttributes } from "@tiptap/core";
import { formatSlashCommandLabel } from "../extensions/slash-command-utils";
import { escapeMarkdownLabel } from "../utils/escape-markdown-label";

export const SlashCommandSchema = Mention.extend({
  name: "slashCommand",

  renderHTML({ node, HTMLAttributes }) {
    return [
      "span",
      mergeAttributes(
        { "data-type": "slash-command" },
        this.options.HTMLAttributes,
        HTMLAttributes,
      ),
      `/${formatSlashCommandLabel(node.attrs.label)}`,
    ];
  },

  addAttributes() {
    return { ...this.parent?.() };
  },

  parseHTML() {
    return [{ tag: 'span[data-type="slash-command"]' }];
  },

  markdownTokenizer: {
    name: "slashCommand",
    level: "inline" as const,
    start(src: string) {
      // Backslash is excluded from the char class so "\x" runs can only be
      // consumed by \\. — overlapping alternatives backtrack in 2^n ways
      // (ReDoS, GitHub #4881).
      return src.search(/\[\/(?:\\.|[^\]\\])+\]\(slash:\/\/skill\//);
    },
    tokenize(src: string) {
      const match = src.match(
        /^\[\/((?:\\.|[^\]\\])+)\]\(slash:\/\/skill\/([^)]+)\)/,
      );
      if (!match) return undefined;
      // Reverse escapeMarkdownLabel: unescape \[ \] \\ \( \). Must mirror the
      // escaped set exactly, or a label containing "\" fails to round-trip
      // through the linear tokenizer.
      const rawLabel = match[1]?.replace(/\\([[\]\\()])/g, "$1");
      return {
        type: "slashCommand",
        raw: match[0],
        attributes: { label: rawLabel, id: match[2] },
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("slashCommand", token.attributes);
  },

  renderMarkdown: (node: any) => {
    const { id, label } = node.attrs || {};
    // Escape [ ] \ ( ) so the label survives the linear tokenizer; must stay in
    // sync with the unescape in tokenize() above.
    const safeLabel = escapeMarkdownLabel(formatSlashCommandLabel(label));
    return `[/${safeLabel}](slash://skill/${id})`;
  },
});
