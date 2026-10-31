"use client";

/**
 * Collapsible `<details>` blocks.
 *
 * The stored form is the HTML GitHub and agents already write, with the body
 * as Markdown between blank lines so ReadonlyContent (rehype-raw) renders the
 * same collapsible:
 *
 *   <details>
 *   <summary>Full log</summary>
 *
 *   ...markdown...
 *
 *   </details>
 *
 * Without these nodes the editor flattened the block into loose paragraphs on
 * load, and the next save persisted the flattened form.
 *
 * Open/closed in the editor is view state only; the `open` attribute is kept
 * as authored so expanding a block never rewrites the document.
 */

import { useEffect, useState } from "react";
import { Node, mergeAttributes, type JSONContent } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import {
  NodeViewContent,
  NodeViewWrapper,
  ReactNodeViewRenderer,
  type NodeViewProps,
} from "@tiptap/react";
import { TextSelection } from "@tiptap/pm/state";
import { ChevronRight } from "lucide-react";
import { useT } from "../../i18n";

const DETAILS_TAG_RE = /<(\/?)details(?=[\s>])[^>]*>/gi;
const DETAILS_OPEN_RE = /^ {0,3}<details(?=[\s>])([^>]*)>/i;
const SUMMARY_RE = /^\s*<summary(?:\s[^>]*)?>([\s\S]*?)<\/summary\s*>/i;
const FENCE_RE = /^ {0,3}(`{3,}|~{3,})/;

/**
 * Offset just past the `</details>` that closes the block opening at the
 * start of `src`, or -1 when it never closes. Nested blocks are counted, and
 * tags inside fenced code are ignored so a code sample mentioning
 * `</details>` cannot end the block early.
 */
export function findDetailsEnd(src: string): number {
  let depth = 0;
  let fence: string | null = null;
  let offset = 0;
  for (const line of src.split("\n")) {
    const fenceMatch = line.match(FENCE_RE);
    if (fence) {
      if (fenceMatch && fenceMatch[1]![0] === fence[0] && fenceMatch[1]!.length >= fence.length) {
        fence = null;
      }
    } else if (fenceMatch) {
      fence = fenceMatch[1]!;
    } else {
      DETAILS_TAG_RE.lastIndex = 0;
      let tag: RegExpExecArray | null;
      while ((tag = DETAILS_TAG_RE.exec(line)) !== null) {
        depth += tag[1] ? -1 : 1;
        if (depth === 0) return offset + tag.index + tag[0].length;
      }
    }
    offset += line.length + 1;
  }
  return -1;
}

const HTML_ENTITIES: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  "#39": "'",
  nbsp: " ",
};

/** Summary HTML → the plain text the summary node holds. */
function summaryText(html: string): string {
  return html
    .replace(/<[^>]*>/g, "")
    .replace(/&(amp|lt|gt|quot|#39|nbsp);/g, (_m, name: string) => HTML_ENTITIES[name] ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function plainText(node: JSONContent | undefined): string {
  if (!node) return "";
  if (node.type === "text") return node.text ?? "";
  return (node.content ?? []).map(plainText).join("");
}

function hasEmptyBody(node: ProseMirrorNode): boolean {
  const body = node.lastChild;
  return !body || (body.childCount === 1 && body.firstChild!.content.size === 0);
}

function DetailsView({ node, editor, getPos }: NodeViewProps) {
  const { t } = useT("editor");
  // A freshly inserted block starts expanded so its body can be typed into.
  const [open, setOpen] = useState(() => Boolean(node.attrs.open) || hasEmptyBody(node));
  const summaryEmpty = (node.firstChild?.content.size ?? 0) === 0;

  // Keyboard navigation into a collapsed body expands it, so the caret never
  // sits in hidden text.
  useEffect(() => {
    if (open) return;
    const reveal = () => {
      const pos = getPos();
      if (typeof pos !== "number") return;
      const current = editor.state.doc.nodeAt(pos);
      if (!current?.firstChild) return;
      const bodyStart = pos + 1 + current.firstChild.nodeSize;
      const { from } = editor.state.selection;
      if (from > bodyStart && from < pos + current.nodeSize) setOpen(true);
    };
    editor.on("selectionUpdate", reveal);
    return () => {
      editor.off("selectionUpdate", reveal);
    };
  }, [editor, getPos, open]);
  return (
    <NodeViewWrapper
      as="div"
      className="details-node"
      data-type="details"
      data-open={open ? "" : undefined}
    >
      <button
        type="button"
        contentEditable={false}
        className="details-toggle"
        aria-expanded={open}
        aria-label={t(($) => (open ? $.details.collapse : $.details.expand))}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => setOpen((value) => !value)}
      >
        <ChevronRight className="size-3.5" />
      </button>
      {summaryEmpty && (
        <span contentEditable={false} className="details-summary-placeholder">
          {t(($) => $.details.summary_placeholder)}
        </span>
      )}
      <NodeViewContent className="details-body" />
    </NodeViewWrapper>
  );
}

export const DetailsSummaryExtension = Node.create({
  name: "detailsSummary",
  content: "text*",
  marks: "",
  defining: true,
  isolating: true,
  selectable: false,

  parseHTML() {
    return [{ tag: "summary" }, { tag: 'div[data-type="detailsSummary"]' }];
  },

  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-type": "detailsSummary" }), 0];
  },

  // Reached only for a partial copy (a slice whose details parent is open);
  // a whole block is written by DetailsExtension.renderMarkdown.
  renderMarkdown: (node: JSONContent) => escapeHtml(plainText(node)),

  addKeyboardShortcuts() {
    return {
      // Enter ends the summary: the caret moves to the start of the body.
      Enter: ({ editor }) => {
        const { $from, empty } = editor.state.selection;
        if (!empty || $from.parent.type.name !== this.name) return false;
        const bodyStart = $from.after() + 2;
        if (bodyStart > editor.state.doc.content.size) return false;
        const tr = editor.state.tr.setSelection(TextSelection.near(editor.state.doc.resolve(bodyStart)));
        editor.view.dispatch(tr.scrollIntoView());
        return true;
      },
    };
  },
});

export const DetailsContentExtension = Node.create({
  name: "detailsContent",
  content: "block+",
  defining: true,
  selectable: false,

  parseHTML() {
    return [{ tag: 'div[data-type="detailsContent"]' }];
  },

  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-type": "detailsContent" }), 0];
  },

  renderMarkdown: (node: JSONContent, helpers: any) =>
    helpers.renderChildren(node.content ?? [], "\n\n"),
});

export const DetailsExtension = Node.create({
  name: "details",
  group: "block",
  content: "detailsSummary detailsContent",
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      open: {
        default: false,
        parseHTML: (element: HTMLElement) => element.hasAttribute("open"),
        renderHTML: (attrs: Record<string, unknown>) => (attrs.open ? { open: "" } : {}),
      },
    };
  },

  parseHTML() {
    return [{ tag: "details" }];
  },

  renderHTML({ HTMLAttributes }) {
    return ["details", HTMLAttributes, 0];
  },

  markdownTokenizer: {
    name: "details",
    level: "block" as const,
    start(src: string) {
      return src.search(/^ {0,3}<details(?=[\s>])/im);
    },
    tokenize(src: string, _tokens: unknown, helpers: any) {
      const opening = src.match(DETAILS_OPEN_RE);
      if (!opening) return undefined;
      const end = findDetailsEnd(src);
      if (end === -1) return undefined;
      const closeStart = src.lastIndexOf("<", end - 1);
      const inner = src.slice(opening[0].length, closeStart);
      const summary = inner.match(SUMMARY_RE);
      const body = summary ? inner.slice(summary[0].length) : inner;
      const raw = src.slice(0, end) + (src[end] === "\n" ? "\n" : "");
      return {
        type: "details",
        raw,
        open: /(^|\s)open(\s|=|$)/i.test(opening[1] ?? ""),
        summary: summary ? summaryText(summary[1] ?? "") : "",
        tokens: helpers.blockTokens(body.trim()),
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) => {
    const children: JSONContent[] = helpers.parseChildren(token.tokens ?? []);
    return helpers.createNode("details", { open: Boolean(token.open) }, [
      helpers.createNode(
        "detailsSummary",
        {},
        token.summary ? [helpers.createTextNode(token.summary)] : [],
      ),
      helpers.createNode(
        "detailsContent",
        {},
        children.length > 0 ? children : [helpers.createNode("paragraph")],
      ),
    ]);
  },

  renderMarkdown: (node: JSONContent, helpers: any) => {
    const [summary, content] = node.content ?? [];
    const body = content ? helpers.renderChildren(content.content ?? [], "\n\n").trim() : "";
    const opening = node.attrs?.open ? "<details open>" : "<details>";
    const head = `${opening}\n<summary>${escapeHtml(plainText(summary))}</summary>`;
    return body ? `${head}\n\n${body}\n\n</details>` : `${head}\n\n</details>`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(DetailsView);
  },
});

/** Content for a new, empty details block (block menu insert). */
export function emptyDetailsContent(): JSONContent {
  return {
    type: "details",
    content: [
      { type: "detailsSummary" },
      { type: "detailsContent", content: [{ type: "paragraph" }] },
    ],
  };
}
