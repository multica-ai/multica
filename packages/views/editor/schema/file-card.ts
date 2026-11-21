/**
 * fileCard node schema and Markdown codec, without the React NodeView, so the
 * collaboration server can parse and serialize documents in Node.
 *
 * Markdown serialization: `!file[filename](href)` — custom syntax that is
 * unambiguous (standard `[name](url)` is indistinguishable from regular links).
 */
import { Node, mergeAttributes } from "@tiptap/core";
import { FILE_CARD_URL_PATTERN } from "@multica/ui/markdown/file-cards";
import { escapeMarkdownLabel } from "../utils/escape-markdown-label";

// Backslash is excluded from the label char class so "\x" runs can only be
// consumed by \\. — overlapping alternatives backtrack in 2^n ways (ReDoS,
// GitHub #4881).
const FILE_CARD_MARKDOWN_RE = new RegExp(
  `^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)`,
);

export const FileCardSchema = Node.create({
  name: "fileCard",
  group: "block",
  atom: true,

  addAttributes() {
    return {
      href: {
        default: "",
        rendered: false, // Don't put href on DOM — prevents link behavior
      },
      filename: {
        default: "",
        rendered: false,
      },
      fileSize: {
        default: 0,
        rendered: false,
      },
      uploading: {
        default: false,
        rendered: false,
      },
      uploadId: {
        default: null,
        rendered: false,
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'div[data-type="fileCard"]',
        getAttrs: (el) => ({
          href: (el as HTMLElement).getAttribute("data-href"),
          filename: (el as HTMLElement).getAttribute("data-filename"),
        }),
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-type": "fileCard",
        "data-href": node.attrs.href,
        "data-filename": node.attrs.filename,
      }),
    ];
  },

  // Markdown: custom !file[name](url) syntax for unambiguous roundtrip.
  // Standard [name](url) is indistinguishable from regular links — the old
  // regex-based CDN hostname matching in preprocessFileCards was fragile.
  markdownTokenizer: {
    name: "fileCard",
    level: "block" as const,
    start(src: string) {
      return src.search(/^!file\[/m);
    },
    tokenize(src: string) {
      const match = src.match(FILE_CARD_MARKDOWN_RE);
      if (!match) return undefined;
      const filename = (match[1] ?? "").replace(/\\([[\]\\()])/g, "$1");
      return {
        type: "fileCard",
        raw: match[0],
        attributes: { filename, href: match[2] },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("fileCard", token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { href, filename, uploading } = node.attrs || {};
    // An in-flight placeholder is not content. The document IS the draft body
    // (getMarkdown → setDraft), so serialising it would persist `!file[x](
    // )` — a line the tokenizer above cannot parse back, leaving dead literal
    // text in the draft that outlives the upload and ships with the comment.
    // Emitting nothing is the whole guarantee: the card becomes content the
    // moment it has a real href, and never before.
    if (uploading === true || !href) return "";
    return `!file[${escapeMarkdownLabel(filename || "file")}](${href})`;
  },
});
