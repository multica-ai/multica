import { Node, mergeAttributes } from "@tiptap/core";

/**
 * The editable caption below an inline image (FIR-4699 Phase 5).
 *
 * A SEPARATE block node, not a child of the image node — so the image node
 * stays a leaf and every un-captioned image still serialises byte-for-byte to
 * `![alt](url)` exactly as today. The caption is a `<figcaption>` in both the
 * editor and the stored Markdown, styled in `--muted-foreground` by
 * `packages/views/editor/styles/cerebro-overrides.css`.
 *
 * Round-trip is entirely the HTML-token path: `@tiptap/markdown` emits whatever
 * `renderMarkdown` returns verbatim, and re-parses the `<figcaption>` block back
 * through `parseHTML` below — the same mechanism the sized `<img>` uses. The
 * caption's inline text is serialised by `helpers.renderChildren`.
 */
export const ImageCaption = Node.create({
  name: "imageCaption",
  group: "block",
  content: "inline*",
  // A caption edits as its own block; keep it whole rather than merging into a
  // neighbouring paragraph on backspace/replace.
  defining: true,
  parseHTML() {
    return [{ tag: "figcaption" }];
  },
  renderHTML({ HTMLAttributes }) {
    return [
      "figcaption",
      mergeAttributes(HTMLAttributes, { class: "image-caption" }),
      0,
    ];
  },
  renderMarkdown: (node, helpers) =>
    `<figcaption>${helpers.renderChildren(node)}</figcaption>`,
});
