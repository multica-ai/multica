"use client";

/**
 * DetailsBlock — Tiptap node extension for collapsible `<details>`/`<summary>`
 * blocks (the GitHub-flavored disclosure widget autopilots emit).
 *
 * Why this exists
 * ---------------
 * Autopilot descriptions and issue *comments* render through `ReadonlyContent`
 * (react-markdown + rehype-raw + rehype-sanitize), whose sanitize schema already
 * allows `<details>`/`<summary>`, so those collapse correctly. The issue
 * *description*, however, renders through the Tiptap `ContentEditor`. ProseMirror
 * had no node for `<details>`, so `@tiptap/markdown` dropped the tags — both on
 * load (the block flattened into its raw text) AND on save (the block was
 * silently destroyed the moment anyone edited the description).
 *
 * Approach (modeled on the math / file-card atom nodes)
 * -----------------------------------------------------
 * A block-level `markdownTokenizer` captures a whole
 * `<details><summary>…</summary>…</details>` block and stores the summary and
 * the inner body markdown **verbatim** as attributes. `renderMarkdown` re-emits
 * the exact same block, so an edit elsewhere in the description round-trips this
 * block instead of losing it. The NodeView renders the body collapsibly through
 * the SAME `ReadonlyContent` renderer the autopilot view uses, so a collapsed
 * block on an issue looks identical to the autopilot view.
 *
 * Trade-off (consistent with the other atom nodes): the body is edited as a unit
 * (via markdown source) rather than inline. That fits real usage — these blocks
 * are authored by autopilots and mostly *viewed* on generated issues; the
 * priority is safe round-trip over in-place authoring.
 *
 * Known limitation: a `<details>` nested inside another `<details>` is not parsed
 * as collapsible — the tokenizer declines it (the inner block's `</details>`
 * would close the outer one), so it falls back to the previous flattened
 * rendering rather than breaking the round-trip.
 */

import { Node, mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { ReadonlyContent } from "../readonly-content";

// Matches a complete <details><summary>…</summary>…</details> block at the
// start of the remaining source. Group 1 = the raw `<details>` attributes (used
// only to detect the `open` flag); group 2 = summary inner text; group 3 = the
// body between </summary> and </details>.
const DETAILS_BLOCK_RE =
  /^<details((?:\s[^>]*)?)>[ \t]*\n?[ \t]*<summary>([\s\S]*?)<\/summary>([\s\S]*?)<\/details>[ \t]*(?:\n|$)/;

// ---------------------------------------------------------------------------
// React NodeView — a native <details> so the browser handles the collapse,
// with the body rendered through the shared ReadonlyContent markdown renderer.
// ---------------------------------------------------------------------------

export function DetailsBlockView({ node }: NodeViewProps) {
  const summary = String(node.attrs.summary ?? "");
  const body = String(node.attrs.body ?? "");
  const open = Boolean(node.attrs.open);

  return (
    <NodeViewWrapper
      as="div"
      className="details-block-node"
      data-type="detailsBlock"
    >
      <details className="details-block" open={open} contentEditable={false}>
        <summary className="details-block-summary">{summary || "Details"}</summary>
        <div className="details-block-body">
          <ReadonlyContent content={body} />
        </div>
      </details>
    </NodeViewWrapper>
  );
}

// ---------------------------------------------------------------------------
// Tiptap Node Extension
// ---------------------------------------------------------------------------

export const DetailsBlockExtension = Node.create({
  name: "detailsBlock",
  group: "block",
  atom: true,
  defining: true,
  isolating: true,
  selectable: true,

  addAttributes() {
    return {
      // The three attributes are the source of truth for both rendering and
      // markdown serialization; none are reflected onto the DOM by ProseMirror
      // (the NodeView owns the DOM), so `rendered: false`.
      summary: { default: "", rendered: false },
      body: { default: "", rendered: false },
      open: { default: false, rendered: false },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'div[data-type="detailsBlock"]',
        getAttrs: (el) => {
          const node = el as HTMLElement;
          return {
            summary: node.getAttribute("data-summary") ?? "",
            body: node.getAttribute("data-body") ?? "",
            open: node.hasAttribute("data-open"),
          };
        },
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-type": "detailsBlock",
        "data-summary": node.attrs.summary,
        "data-body": node.attrs.body,
        ...(node.attrs.open ? { "data-open": "" } : {}),
      }),
    ];
  },

  // Capture the whole <details> block before @tiptap/markdown's default HTML
  // handling flattens it. Declining (returning undefined) leaves the raw HTML
  // to the previous fallback path.
  markdownTokenizer: {
    name: "detailsBlock",
    level: "block" as const,
    start(src: string) {
      return src.search(/^<details[\s>]/m);
    },
    tokenize(src: string) {
      const match = src.match(DETAILS_BLOCK_RE);
      if (!match) return undefined;
      const body = (match[3] ?? "").trim();
      // Nested <details> would have matched the inner block's </details>,
      // leaving a dangling opener in the body. Decline so it falls back to
      // the flattened rendering instead of producing a broken round-trip.
      if (/<details[\s>]/.test(body)) return undefined;
      return {
        type: "detailsBlock",
        raw: match[0],
        attributes: {
          summary: (match[2] ?? "").trim(),
          body,
          open: /\bopen\b/.test(match[1] ?? ""),
        },
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("detailsBlock", token.attributes);
  },

  renderMarkdown: (node: any) => {
    const summary = String(node.attrs?.summary ?? "").trim();
    const body = String(node.attrs?.body ?? "").trim();
    const open = node.attrs?.open ? " open" : "";
    return `<details${open}>\n<summary>${summary}</summary>\n\n${body}\n</details>`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(DetailsBlockView);
  },
});
