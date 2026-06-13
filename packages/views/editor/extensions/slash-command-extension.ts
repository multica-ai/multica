import Mention from "@tiptap/extension-mention";
import { mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { SlashCommandView } from "./slash-command-view";
import { formatSlashCommandLabel } from "./slash-command-utils";

export const SlashCommandExtension = Mention.extend({
  name: "slashCommand",

  addNodeView() {
    return ReactNodeViewRenderer(SlashCommandView);
  },

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
      return src.search(/\[\/(?:\\.|[^\]])+\]\(slash:\/\/skill\//);
    },
    tokenize(src: string) {
      const match = src.match(
        /^\[\/((?:\\.|[^\]])+)\]\(slash:\/\/skill\/([^)]+)\)/,
      );
      if (!match) return undefined;
      const rawLabel = match[1]?.replace(/\\\[/g, "[").replace(/\\\]/g, "]");
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
    const safeLabel = formatSlashCommandLabel(label)
      .replace(/\[/g, "\\[")
      .replace(/\]/g, "\\]");
    return `[/${safeLabel}](slash://skill/${id})`;
  },
});
