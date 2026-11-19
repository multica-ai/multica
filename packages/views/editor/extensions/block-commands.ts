import type { ChainedCommands, Editor, Range } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { TextSelection } from "@tiptap/pm/state";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { uploadAndInsertFile } from "./file-upload";
import { armSuggestionTrigger } from "./suggestion-trigger-arming";

/**
 * Block-level authoring commands shared by the `/` insert menu and the block
 * handle. Every block here has a plain Markdown form, so what an agent reads
 * back through `doc get` is exactly what the author saw.
 */

/** Block types a text block can be turned into. */
export type TextBlockKind =
  | "paragraph"
  | "heading1"
  | "heading2"
  | "heading3"
  | "bulletList"
  | "orderedList"
  | "taskList"
  | "blockquote"
  | "codeBlock";

/** Everything the `/` menu can insert. */
export type InsertBlockKind =
  | TextBlockKind
  | "mermaid"
  | "table"
  | "divider"
  | "image"
  | "file";

export const TEXT_BLOCK_KINDS: readonly TextBlockKind[] = [
  "paragraph",
  "heading1",
  "heading2",
  "heading3",
  "bulletList",
  "orderedList",
  "taskList",
  "blockquote",
  "codeBlock",
];

export type UploadHandler = (
  file: File,
  uploadId: string,
) => Promise<UploadResult | null>;

/**
 * Lift the selection out of lists and quotes, then give it `kind`. Clearing
 * first is what makes "turn into" predictable: a list item turned into a
 * heading leaves the list instead of failing on the list's content rule.
 */
function applyTextBlock(chain: ChainedCommands, kind: TextBlockKind): ChainedCommands {
  const cleared = chain.clearNodes();
  switch (kind) {
    case "paragraph":
      return cleared;
    case "heading1":
      return cleared.setHeading({ level: 1 });
    case "heading2":
      return cleared.setHeading({ level: 2 });
    case "heading3":
      return cleared.setHeading({ level: 3 });
    case "bulletList":
      return cleared.toggleBulletList();
    case "orderedList":
      return cleared.toggleOrderedList();
    case "taskList":
      return cleared.toggleTaskList();
    case "blockquote":
      return cleared.toggleBlockquote();
    case "codeBlock":
      return cleared.setCodeBlock();
  }
}

/** Open the platform file picker; resolves with the chosen files (possibly none). */
export function pickFiles(accept?: string): Promise<File[]> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = true;
    if (accept) input.accept = accept;
    input.addEventListener("change", () => resolve(Array.from(input.files ?? [])), {
      once: true,
    });
    input.addEventListener("cancel", () => resolve([]), { once: true });
    input.click();
  });
}

/**
 * Replace the typed `/query` at `range` with a block of `kind`. Image and file
 * open the file picker and upload at the caret, the same path as a paste.
 */
export function insertBlock(
  editor: Editor,
  range: Range,
  kind: InsertBlockKind,
  options: {
    upload?: UploadHandler;
    pick?: (accept?: string) => Promise<File[]>;
  } = {},
): void {
  const chain = editor.chain().focus().deleteRange(range);
  switch (kind) {
    case "mermaid":
      chain.clearNodes().setCodeBlock({ language: "mermaid" }).run();
      return;
    case "table":
      chain.insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run();
      return;
    case "divider":
      chain.setHorizontalRule().run();
      return;
    case "image":
    case "file": {
      chain.run();
      const upload = options.upload;
      if (!upload) return;
      const pick = options.pick ?? pickFiles;
      void pick(kind === "image" ? "image/*" : undefined).then(async (files) => {
        for (const file of files) {
          if (editor.isDestroyed) return;
          await uploadAndInsertFile(editor, file, upload);
        }
      });
      return;
    }
    default:
      applyTextBlock(chain, kind).run();
  }
}

/** A block the handle acts on: a list item, or a direct child of the document. */
export interface BlockTarget {
  pos: number;
  node: ProseMirrorNode;
}

const LIST_ITEM_TYPES = new Set(["listItem", "taskItem"]);

/**
 * The block the handle should act on for document position `pos`: the
 * innermost list item around it, otherwise the top-level block containing it.
 * List items get their own handle because moving one item is far more common
 * than moving a whole list.
 */
export function resolveBlockTarget(
  doc: ProseMirrorNode,
  pos: number,
): BlockTarget | null {
  if (pos < 0 || pos > doc.content.size) return null;
  const $pos = doc.resolve(pos);
  for (let depth = $pos.depth; depth > 0; depth--) {
    const node = $pos.node(depth);
    if (LIST_ITEM_TYPES.has(node.type.name)) {
      return { pos: $pos.before(depth), node };
    }
  }
  if ($pos.depth >= 1) return { pos: $pos.before(1), node: $pos.node(1) };
  // Depth 0: `pos` sits in front of a top-level leaf such as an image.
  const node = doc.nodeAt(pos);
  return node ? { pos, node } : null;
}

const TURNABLE_TYPES = new Set([
  "paragraph",
  "heading",
  "codeBlock",
  "blockquote",
  "listItem",
  "taskItem",
  "bulletList",
  "orderedList",
  "taskList",
]);

/** Whether "turn into" applies; tables, images and other leaves keep their type. */
export function canTurnInto(node: ProseMirrorNode): boolean {
  return TURNABLE_TYPES.has(node.type.name);
}

/** The text block kind a block currently has, for marking it in the menu. */
export function currentTextBlockKind(node: ProseMirrorNode): TextBlockKind | null {
  switch (node.type.name) {
    case "paragraph":
      return "paragraph";
    case "heading": {
      const level = node.attrs.level as number;
      return level === 1 ? "heading1" : level === 2 ? "heading2" : level === 3 ? "heading3" : null;
    }
    case "codeBlock":
      return "codeBlock";
    case "blockquote":
      return "blockquote";
    case "bulletList":
      return "bulletList";
    case "orderedList":
      return "orderedList";
    case "taskList":
    case "taskItem":
      return "taskList";
    default:
      return null;
  }
}

function selectBlockText(editor: Editor, target: BlockTarget) {
  const { state, view } = editor;
  const selection = TextSelection.between(
    state.doc.resolve(target.pos),
    state.doc.resolve(target.pos + target.node.nodeSize),
  );
  view.dispatch(state.tr.setSelection(selection));
}

export function turnBlockInto(
  editor: Editor,
  target: BlockTarget,
  kind: TextBlockKind,
): void {
  if (!canTurnInto(target.node)) return;
  selectBlockText(editor, target);
  applyTextBlock(editor.chain().focus(), kind).run();
  // Leave a caret, not the selected text: a selection would keep the
  // formatting toolbar open over the block.
  editor.commands.setTextSelection(editor.state.selection.to);
}

export function duplicateBlock(editor: Editor, target: BlockTarget): void {
  const end = target.pos + target.node.nodeSize;
  editor.view.dispatch(editor.state.tr.insert(end, target.node).scrollIntoView());
}

export function deleteBlock(editor: Editor, target: BlockTarget): void {
  editor.view.dispatch(
    editor.state.tr.deleteRange(target.pos, target.pos + target.node.nodeSize),
  );
}

/** Whether a block holds nothing but one empty paragraph's worth of text. */
function emptyTextStart(target: BlockTarget): number | null {
  const { node, pos } = target;
  if (node.isTextblock && node.content.size === 0) return pos + 1;
  if (
    LIST_ITEM_TYPES.has(node.type.name) &&
    node.childCount === 1 &&
    node.firstChild?.isTextblock &&
    node.firstChild.content.size === 0
  ) {
    return pos + 2;
  }
  return null;
}

/**
 * The handle's "+": open the insert menu on a new line below `target` (or on
 * `target` itself when it is already an empty line). A list item gets a sibling
 * item, so adding below an item stays inside its list.
 */
export function openInsertMenuBelow(editor: Editor, target: BlockTarget): void {
  const { state, view } = editor;
  const tr = state.tr;
  let caret = emptyTextStart(target);
  if (caret === null) {
    const end = target.pos + target.node.nodeSize;
    const sibling = LIST_ITEM_TYPES.has(target.node.type.name)
      ? target.node.type.createAndFill()
      : state.schema.nodes.paragraph!.create();
    if (!sibling) return;
    tr.insert(end, sibling);
    caret = sibling.isTextblock ? end + 1 : end + 2;
  }
  tr.setSelection(TextSelection.create(tr.doc, caret));
  tr.insertText("/");
  armSuggestionTrigger(tr, caret);
  view.dispatch(tr.scrollIntoView());
  view.focus();
}
