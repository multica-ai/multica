import { TextSelection } from "@tiptap/pm/state";
import type { Editor } from "@tiptap/core";

/** Minimal shape of a ProseMirror JSON node — enough to walk and patch. */
interface JsonNode {
  type?: string;
  content?: JsonNode[];
  [key: string]: unknown;
}

/**
 * Ensure every list item starts with a paragraph.
 *
 * `listItem`/`taskItem` content is `paragraph block*`, so a valid item's first
 * child must be a paragraph. `@tiptap/markdown` violates that two ways for an
 * empty item: a top-level empty item (`"1. "`) parses to a childless item, and
 * a nested one (`"1. \n   1. "`) parses to an outer item whose only child is the
 * sub-list. Both are schema-invalid and leave the document without a text
 * cursor. Recurse depth-first, then prepend an empty paragraph to any item that
 * doesn't already lead with one. Attrs (e.g. a task item's `checked`) are kept.
 */
function repairListItems(node: JsonNode): { node: JsonNode; changed: boolean } {
  let changed = false;
  let content = node.content;

  if (content) {
    const mapped = content.map((child) => {
      const result = repairListItems(child);
      if (result.changed) changed = true;
      return result.node;
    });
    if (changed) content = mapped;
  }

  if (node.type === "listItem" || node.type === "taskItem") {
    if (!content || content.length === 0 || content[0]?.type !== "paragraph") {
      content = [{ type: "paragraph" }, ...(content ?? [])];
      changed = true;
    }
  }

  return changed ? { node: { ...node, content }, changed } : { node, changed };
}

/**
 * Repair markdown-parsed empty list items so they hold a valid text cursor.
 *
 * The document left after typing `1.` (or `- `) in a comment is persisted as the
 * draft `"1. \n\n"`. On remount `@tiptap/markdown` parses that empty item into a
 * schema-invalid, childless `listItem` (see {@link repairListItems}); the
 * document is then left with an `AllSelection` instead of a cursor, so the
 * browser paints the caret on the following block — the reported bug: "type
 * `1.`, switch issues, come back, the caret jumps off the list item and can't be
 * moved back." Note this only affects the round-trip of an *empty* item;
 * non-empty items (`"1. buy milk"`) round-trip byte-identically.
 *
 * `preferredSelection` (the caret captured before an external re-parse) is
 * restored when given, snapped to a valid text position; otherwise the caret
 * lands at the first valid text position (the list item's paragraph). No-op when
 * every list item is already valid, so ordinary content is untouched.
 *
 * Returns true when it repaired the document (and therefore set the caret).
 */
export function repairEmptyListItems(
  editor: Editor,
  preferredSelection?: { from: number; to: number },
): boolean {
  if (editor.isDestroyed) return false;

  const { node: repaired, changed } = repairListItems(
    editor.getJSON() as JsonNode,
  );
  if (!changed) return false;

  // The corrupt parse leaves an `AllSelection`. Re-setting content while that
  // selection is live makes ProseMirror's replace collapse the whole list into
  // bare paragraphs, so first pin the selection to a real position.
  editor.view.dispatch(
    editor.state.tr
      .setSelection(TextSelection.near(editor.state.doc.resolve(0)))
      .setMeta("addToHistory", false),
  );

  // Rebuild from the repaired tree. This is a normalization of externally
  // parsed content, not a user edit: `setMeta("addToHistory", false)` keeps it
  // off the undo stack (otherwise Ctrl-Z would restore the broken item), and
  // `emitUpdate: false` avoids a self-write back to the draft/server.
  editor
    .chain()
    .setMeta("addToHistory", false)
    .setContent(repaired, { emitUpdate: false })
    .run();

  const { doc } = editor.state;
  const size = doc.content.size;
  const clamp = (pos: number) => Math.min(Math.max(pos, 0), size);
  const anchor = preferredSelection ? clamp(preferredSelection.from) : 0;
  const head = preferredSelection ? clamp(preferredSelection.to) : 0;
  editor.view.dispatch(
    editor.state.tr
      .setSelection(TextSelection.between(doc.resolve(anchor), doc.resolve(head)))
      .setMeta("addToHistory", false),
  );
  return true;
}
