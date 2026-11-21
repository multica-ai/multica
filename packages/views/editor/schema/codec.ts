/**
 * Markdown ⇄ collaborative document conversion, for the collaboration server.
 *
 * Markdown stays the canonical stored format; the Yjs document is the live
 * editing state. Every conversion here goes through the same schema and
 * Markdown codec the editor uses, so a document loaded on the server is the
 * one a browser would have built from the same Markdown.
 *
 * HTML inside Markdown (including the file-card divs preprocessMarkdown
 * produces) is only parsed when a DOM is available; the server installs one
 * before using the codec.
 */
import { createDocument, getSchema, type JSONContent } from "@tiptap/core";
import { MarkdownManager } from "@tiptap/markdown";
import type { Node as PMNode, Schema } from "@tiptap/pm/model";
import * as Y from "yjs";
import {
  prosemirrorJSONToYXmlFragment,
  updateYFragment,
  yXmlFragmentToProseMirrorRootNode,
} from "@tiptap/y-tiptap";
import { preprocessMarkdown } from "../utils/preprocess";
import {
  MARKDOWN_CHUNK_THRESHOLD,
  parseMarkdownChunked,
} from "../utils/parse-markdown-chunked";
import {
  COLLAB_FIELD,
  createMarkdownSchemaExtensions,
  createMarkedInstance,
  MARKDOWN_OPTIONS,
} from "./index";

export { COLLAB_FIELD };
export type { JSONContent };

export interface DocumentCodec {
  schema: Schema;
  /** Stored Markdown → ProseMirror JSON, with the editor's preprocessing. */
  parse(markdown: string): JSONContent;
  /** ProseMirror JSON → Markdown, normalized the way the editor saves it. */
  serialize(json: JSONContent): string;
  /** JSON → schema-checked node; content the schema rejects is dropped. */
  toNode(json: JSONContent): PMNode;
  /** A fresh Y.Doc holding `markdown`. */
  createYDoc(markdown: string): Y.Doc;
  ydocToJSON(ydoc: Y.Doc): JSONContent;
  ydocToMarkdown(ydoc: Y.Doc): string;
  /** Rewrites the live document to `json` as a structural diff in one
   *  transaction: unchanged blocks keep their Yjs identity, so collaborators'
   *  carets and concurrent edits elsewhere are left alone. */
  applyJSON(ydoc: Y.Doc, json: JSONContent, origin?: unknown): void;
}

export function createDocumentCodec(opts: { cdnDomain?: string } = {}): DocumentCodec {
  const extensions = createMarkdownSchemaExtensions();
  const schema = getSchema(extensions);
  const manager = new MarkdownManager({ ...MARKDOWN_OPTIONS, marked: createMarkedInstance(), extensions });
  const cdnDomain = opts.cdnDomain ?? "";

  const parse = (markdown: string): JSONContent => {
    const source = preprocessMarkdown(markdown, { cdnDomain });
    if (!source.trim()) return { type: "doc", content: [{ type: "paragraph" }] };
    return source.length > MARKDOWN_CHUNK_THRESHOLD
      ? parseMarkdownChunked(manager, source)
      : manager.parse(source);
  };
  const toNode = (json: JSONContent): PMNode =>
    createDocument(json, schema, undefined, { errorOnInvalidContent: false });
  const serialize = (json: JSONContent): string => manager.serialize(json).trim();
  // The editor keeps an empty paragraph after a final list, table or code
  // block, and every connected editor adds its own when it is missing: a
  // server-side rewrite without one would come back with one per editor,
  // saved as `&nbsp;` lines. A single one saves as nothing.
  const withTrailingParagraph = (json: JSONContent): JSONContent =>
    json.content?.at(-1)?.type === "paragraph"
      ? json
      : { ...json, content: [...(json.content ?? []), { type: "paragraph" }] };
  const ydocToJSON = (ydoc: Y.Doc): JSONContent =>
    yXmlFragmentToProseMirrorRootNode(ydoc.getXmlFragment(COLLAB_FIELD), schema).toJSON();

  return {
    schema,
    parse,
    serialize,
    toNode,
    createYDoc(markdown) {
      const ydoc = new Y.Doc();
      prosemirrorJSONToYXmlFragment(
        schema,
        toNode(withTrailingParagraph(parse(markdown))).toJSON(),
        ydoc.getXmlFragment(COLLAB_FIELD),
      );
      return ydoc;
    },
    ydocToJSON,
    ydocToMarkdown: (ydoc) => serialize(ydocToJSON(ydoc)),
    applyJSON(ydoc, json, origin) {
      const node = toNode(withTrailingParagraph(json));
      ydoc.transact(() => {
        updateYFragment(ydoc, ydoc.getXmlFragment(COLLAB_FIELD), node, {
          mapping: new Map(),
          isOMark: new Map(),
        });
      }, origin);
    },
  };
}
