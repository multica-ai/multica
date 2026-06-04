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
import type { JSONContent } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { Schema, Slice } from "@tiptap/pm/model";

// Blob URLs (blob:http://…) are process-local; never let them leave the page.
const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;

type MarkdownSerializer = {
  serialize: (doc: JSONContent) => string;
};

function plainTextFromSlice(slice: Slice): string {
  return slice.content.textBetween(0, slice.content.size, "\n\n");
}

function hasOpenCodeBlockBoundary(slice: Slice): boolean {
  const first = slice.content.firstChild;
  const last = slice.content.lastChild;

  return Boolean(
    (slice.openStart > 0 && first?.type.name === "codeBlock") ||
      (slice.openEnd > 0 && last?.type.name === "codeBlock"),
  );
}

export function serializeMarkdownCopyText(
  slice: Slice,
  {
    markdown,
    schema,
  }: {
    markdown?: MarkdownSerializer;
    schema: Schema;
  },
): string {
  const fallback = () => plainTextFromSlice(slice);

  if (!markdown) return fallback();

  // A manual selection that starts or ends inside a code block must copy
  // exactly the visible text selection. Serializing that open slice as
  // Markdown wraps the partial code in fences, so pasting no longer matches
  // the user's blue-highlighted range.
  if (hasOpenCodeBlockBoundary(slice)) return fallback();

  try {
    // Wrap slice content in a temp doc so the serializer walks it like a real
    // document. Inline-only slices auto-wrap into doc → paragraph; block
    // slices pass through.
    const doc = schema.topNodeType.create(null, slice.content);
    const md = markdown.serialize(doc.toJSON());
    return md.replace(BLOB_IMAGE_RE, "").replace(/\n+$/, "");
  } catch {
    // Special selections (e.g. table cellSelection) may fail schema
    // validation when wrapped in a doc node. Fall back so copy never breaks.
    return fallback();
  }
}

export function createMarkdownCopyExtension() {
  return Extension.create({
    name: "markdownCopy",
    addProseMirrorPlugins() {
      const { editor } = this;

      return [
        new Plugin({
          key: new PluginKey("markdownCopy"),
          props: {
            clipboardTextSerializer(slice: Slice) {
              return serializeMarkdownCopyText(slice, {
                markdown: editor.markdown,
                schema: editor.schema,
              });
            },
          },
        }),
      ];
    },
  });
}
