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
 * Editing: the node stays an `atom`, but in an editable editor the NodeView
 * swaps its read-only preview for form controls — an inline `<input>` for the
 * summary and a source-edit `<textarea>` for the body — that write straight
 * back to the `summary`/`body` attributes via `updateAttributes`. Because the
 * attributes remain the verbatim source of truth, an edit still round-trips to
 * `<details>` markdown unchanged. Keeping the body as a markdown source field
 * (rather than promoting it to real ProseMirror content) is deliberate: it
 * preserves that verbatim round-trip for arbitrary bodies (code fences, lists),
 * which real ProseMirror content could not guarantee.
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
  /^<details((?:\s[^>]*)?)>\s*<summary>([\s\S]*?)<\/summary>([\s\S]*?)<\/details>[ \t]*(?:\n|$)/;

// ---------------------------------------------------------------------------
// React NodeView. When the editor is read-only it renders a native <details>
// so the browser handles the collapse, with the body rendered through the
// shared ReadonlyContent markdown renderer. When the editor is editable it
// swaps in form controls that write edits back to the node attributes.
// ---------------------------------------------------------------------------

export function DetailsBlockView({ node, updateAttributes, editor }: NodeViewProps) {
  const summary = String(node.attrs.summary ?? "");
  const body = String(node.attrs.body ?? "");
  const open = Boolean(node.attrs.open);
  const editable = editor?.isEditable ?? false;

  if (!editable) {
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

  // Keep keystrokes and pointer gestures inside the form controls so ProseMirror
  // keymaps (Backspace deleting the node, Enter splitting, etc.) don't fire while
  // the user is typing in a field. The controls live in a contentEditable=false
  // shell, matching the other atom node views.
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <NodeViewWrapper
      as="div"
      className="details-block-node"
      data-type="detailsBlock"
    >
      <div className="details-block details-block--editing" contentEditable={false}>
        <input
          className="details-block-summary-input"
          type="text"
          value={summary}
          placeholder="Details"
          aria-label="Summary"
          onChange={(e) => updateAttributes({ summary: e.target.value })}
          onKeyDown={stop}
          onMouseDown={stop}
        />
        <textarea
          className="details-block-body-input"
          value={body}
          placeholder="Body (Markdown)"
          aria-label="Details body (Markdown)"
          rows={Math.max(3, body.split("\n").length)}
          onChange={(e) => updateAttributes({ body: e.target.value })}
          onKeyDown={stop}
          onMouseDown={stop}
        />
      </div>
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
      // A body ending mid-code-fence means the lazy </details> match truncated
      // inside a fenced sample that itself contained a </details>. An odd fence
      // count is the tell-tale of that truncation — decline so the block falls
      // back to flat rendering instead of leaking an unterminated fence.
      if (((body.match(/```/g) ?? []).length) % 2 !== 0) return undefined;
      return {
        type: "detailsBlock",
        raw: match[0],
        attributes: {
          summary: (match[2] ?? "").trim(),
          body,
          open: /(^|\s)open(\s|=|$)/.test(match[1] ?? ""),
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
