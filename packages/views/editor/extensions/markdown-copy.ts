/**
 * Markdown copy extension — make the clipboard's text/plain channel carry
 * Markdown source instead of plain textContent.
 *
 * Symmetric to markdown-paste.ts:
 *   paste:  text/plain  →  editor.markdown.parse  →  doc
 *   copy:   slice       →  editor.markdown.serialize  →  text/plain
 *
 * Why: ProseMirror's default clipboardTextSerializer calls Slice.textBetween,
 * which flattens every node to its inner text. Headings, lists, code blocks,
 * mentions, file cards — all lose their Markdown markers. Pasting into VS
 * Code, terminals, or messaging apps then sees only naked text.
 *
 * The text/html channel is left at ProseMirror's default so pasting back
 * into another ProseMirror editor still preserves exact node structure via
 * data-pm-slice.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { Slice } from "@tiptap/pm/model";

// Blob URLs (blob:http://…) are process-local; never let them leave the page.
const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;

export function createMarkdownCopyExtension() {
  return Extension.create({
    name: "markdownCopy",
    addProseMirrorPlugins() {
      const { editor } = this;

      const fallback = (slice: Slice) =>
        slice.content.textBetween(0, slice.content.size, "\n\n");

      return [
        new Plugin({
          key: new PluginKey("markdownCopy"),
          props: {
            clipboardTextSerializer(slice: Slice) {
              if (!editor.markdown) return fallback(slice);
              try {
                // Wrap slice content in a temp doc so the serializer walks
                // it like a real document. Inline-only slices auto-wrap
                // into doc → paragraph; block slices pass through.
                const doc = editor.schema.topNodeType.create(
                  null,
                  slice.content,
                );
                const md = editor.markdown.serialize(doc.toJSON());
                return md.replace(BLOB_IMAGE_RE, "").replace(/\n+$/, "");
              } catch {
                // Special selections (e.g. table cellSelection) may fail
                // schema validation when wrapped in a doc node. Fall back
                // so copy never breaks.
                return fallback(slice);
              }
            },
          },
        }),
      ];
    },
  });
}
