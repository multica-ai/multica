import Mention from "@tiptap/extension-mention";
import { mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { MentionView } from "./mention-view";

const MENTION_URL_PREFIX = "](mention://";
const MAX_MENTION_LABEL_SOURCE_LENGTH = 512;

function isEscaped(src: string, index: number): boolean {
  let slashCount = 0;
  for (let i = index - 1; i >= 0 && src[i] === "\\"; i--) slashCount++;
  return slashCount % 2 === 1;
}

function parseMentionMarkdown(src: string) {
  if (src[0] !== "[") return undefined;

  const labelLimit = Math.min(src.length, 1 + MAX_MENTION_LABEL_SOURCE_LENGTH);
  let labelEnd = -1;
  for (let i = 1; i < labelLimit; i++) {
    if (src[i] === "]" && !isEscaped(src, i) && src.startsWith(MENTION_URL_PREFIX, i)) {
      labelEnd = i;
      break;
    }
  }
  if (labelEnd === -1) return undefined;

  const rawLabel = src.slice(1, labelEnd);
  if (!rawLabel) return undefined;

  const typeStart = labelEnd + MENTION_URL_PREFIX.length;
  let slashIndex = typeStart;
  while (slashIndex < src.length && /\w/.test(src[slashIndex] ?? "")) slashIndex++;
  if (slashIndex === typeStart || src[slashIndex] !== "/") return undefined;

  const idStart = slashIndex + 1;
  const idEnd = src.indexOf(")", idStart);
  if (idEnd === -1 || idEnd === idStart) return undefined;

  const labelWithoutPrefix = rawLabel.startsWith("@") ? rawLabel.slice(1) : rawLabel;
  const label = labelWithoutPrefix.replace(/\\\[/g, "[").replace(/\\\]/g, "]");

  return {
    raw: src.slice(0, idEnd + 1),
    label,
    type: src.slice(typeStart, slashIndex),
    id: src.slice(idStart, idEnd),
  };
}

function findMentionMarkdownStart(src: string): number {
  let searchFrom = 0;

  while (searchFrom < src.length) {
    const labelEnd = src.indexOf(MENTION_URL_PREFIX, searchFrom);
    if (labelEnd === -1) return -1;
    if (isEscaped(src, labelEnd)) {
      searchFrom = labelEnd + 1;
      continue;
    }

    const minOpen = Math.max(0, labelEnd - MAX_MENTION_LABEL_SOURCE_LENGTH);
    for (let i = labelEnd - 1; i >= minOpen; i--) {
      if (src[i] !== "[" || isEscaped(src, i)) continue;
      if (parseMentionMarkdown(src.slice(i))) return i;
    }

    searchFrom = labelEnd + MENTION_URL_PREFIX.length;
  }

  return -1;
}

export const BaseMentionExtension = Mention.extend({
  addNodeView() {
    return ReactNodeViewRenderer(MentionView);
  },
  renderHTML({ node, HTMLAttributes }) {
    const type = node.attrs.type ?? "member";
    const prefix = type === "issue" || type === "project" ? "" : "@";
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
  markdownTokenizer: {
    name: "mention",
    level: "inline" as const,
    start(src: string) {
      return findMentionMarkdownStart(src);
    },
    tokenize(src: string) {
      const mention = parseMentionMarkdown(src);
      if (!mention) return undefined;
      return {
        type: "mention",
        raw: mention.raw,
        attributes: { label: mention.label, type: mention.type, id: mention.id },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("mention", token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { id, label, type = "member" } = node.attrs || {};
    const prefix = type === "issue" || type === "project" ? "" : "@";
    // Escape square brackets in the label so the markdown link syntax
    // is not broken when the name contains [ or ] (e.g. "David[TF]").
    const safeLabel = (label ?? id).replace(/\[/g, "\\[").replace(/\]/g, "\\]");
    return `[${prefix}${safeLabel}](mention://${type}/${id})`;
  },
});
