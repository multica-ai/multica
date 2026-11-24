/**
 * The document schema and Markdown codec shared by the editor and the
 * collaboration server.
 *
 * Nothing here may import React, CSS or browser-only modules: the
 * collaboration server loads this file in Node to turn stored Markdown into
 * the collaborative document and back. The editor builds its extension array
 * from the same pieces and adds NodeViews, pickers and input behaviour on top
 * (../extensions/index.ts); schema.test.ts fails when the two drift apart.
 */
import StarterKit, { type StarterKitOptions } from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import TableRow from "@tiptap/extension-table-row";
import TableHeader from "@tiptap/extension-table-header";
import TableCell from "@tiptap/extension-table-cell";
import { Table } from "@tiptap/extension-table";
import { TaskList } from "@tiptap/extension-list";
import { Markdown } from "@tiptap/markdown";
import { Marked, type marked } from "marked";
import type { AnyExtension } from "@tiptap/core";
import { CodeBlockExtension } from "../extensions/code-block";
import { PatchedListItem, PatchedOrderedList, PatchedTaskItem } from "../extensions/list-item";
import { HighlightExtension } from "../extensions/highlight";
import { FileCardSchema } from "./file-card";
import { MentionSchema } from "./mention";
import { BlockMathSchema, InlineMathSchema } from "./math";
import { ImageSchema } from "./image";
import { SlashCommandSchema } from "./slash-command";

export { FileCardSchema } from "./file-card";
export { MentionSchema, mentionText } from "./mention";
export { BlockMathSchema, InlineMathSchema } from "./math";
export { ImageSchema } from "./image";
export { SlashCommandSchema } from "./slash-command";

export const STARTER_KIT_OPTIONS: Partial<StarterKitOptions> = {
  // Every level Markdown can express. The Markdown parser keeps the source
  // depth of `#`…`######`, but Heading.renderHTML falls back to `levels[0]`
  // for any level it was not configured with — so `levels: [1, 2, 3]` made
  // the editor draw every H4–H6 as an H1 (MUL-6060). The same list drives
  // parseHTML, so it also decides whether a pasted `<h4>` survives as a
  // heading. This is about rendering headings the content already has; the
  // bubble menu still offers only H1–H3 as authoring choices.
  heading: { levels: [1, 2, 3, 4, 5, 6] },
  link: false,
  codeBlock: false,
  // Underline has no Markdown representation. Tiptap's extension serializes
  // the mark as `++text++`, which is not CommonMark or GFM, so ReadonlyContent
  // (react-markdown + remark-gfm) renders the delimiters literally. Disabling
  // the extension drops the mark at parse time instead: pasted `<u>` /
  // `text-decoration: underline` keep their text, and Cmd+U becomes a no-op
  // rather than a way to produce content the display layer cannot render.
  underline: false,
  // Disable StarterKit's stock ListItem — its Enter keybind binds only
  // `splitListItem`, which leaves the user stuck inside an empty top-level
  // list item (see list-item.ts). PatchedListItem restores the standard
  // split → lift fallback chain.
  listItem: false,
  // PatchedOrderedList drops indentation the stock parser leaks into items.
  orderedList: false,
};

/** The Y.XmlFragment the Collaboration extension binds. */
export const COLLAB_FIELD = "default";

// 3-space indent so nested ordered lists survive CommonMark in ReadonlyContent.
export const MARKDOWN_OPTIONS = { indentation: { style: "space" as const, size: 3 } };

/**
 * A private marked instance for one editor or codec. Without one,
 * @tiptap/markdown registers every editor's tokenizers on the global `marked`
 * with `use()` and never removes them: the list grows by one editor's worth
 * per mount, every inline parse runs all of them, and each tokenizer keeps its
 * editor, the React refs its extensions close over and the unmounted DOM alive
 * (MUL-9634: ~1,000 DOM nodes retained per issue opened).
 */
export function createMarkedInstance(): typeof marked {
  // A Marked instance has every member MarkdownManager uses (use, Lexer,
  // defaults, lexer, setOptions); the option is typed as the global's.
  return new Marked() as unknown as typeof marked;
}

/** Schema-only extensions: every node and mark the editor can hold, with
 *  their Markdown codecs, and no views, plugins that need a DOM, or pickers. */
export function createMarkdownSchemaExtensions(): AnyExtension[] {
  return [
    StarterKit.configure({ ...STARTER_KIT_OPTIONS, undoRedo: false }),
    PatchedListItem,
    PatchedOrderedList,
    TaskList,
    PatchedTaskItem,
    CodeBlockExtension,
    Link.extend({ inclusive: false }).configure({ openOnClick: false, autolink: false }),
    ImageSchema,
    Table.configure({ resizable: false, renderWrapper: true }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathSchema,
    InlineMathSchema,
    HighlightExtension,
    Markdown.configure({ ...MARKDOWN_OPTIONS, marked: createMarkedInstance() }),
    FileCardSchema,
    MentionSchema,
    SlashCommandSchema,
  ];
}
