/**
 * DetailsBlock node schema and Markdown codec, without the React NodeView, so
 * the collaboration server can parse and serialize `<details>` blocks in Node.
 * See ../extensions/details.tsx for the editor view and the design notes.
 */
import { Node, mergeAttributes } from "@tiptap/core";

// Matches a complete <details><summary>…</summary>…</details> block at the
// start of the remaining source. Group 1 = the raw `<details>` attributes (used
// only to detect the `open` flag); group 2 = summary inner text; group 3 = the
// body between </summary> and </details>.
const DETAILS_BLOCK_RE =
  /^<details((?:\s[^>]*)?)>\s*<summary>([\s\S]*?)<\/summary>([\s\S]*?)<\/details>[ \t]*(?:\n|$)/;

export const DetailsBlockSchema = Node.create({
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
});
